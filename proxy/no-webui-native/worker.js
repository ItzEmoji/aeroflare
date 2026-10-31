// Prefix of the layer mediaType the Go pusher writes for a NAR blob:
// "application/vnd.aeroflare.nar.v1+<compression>" (see internal/push/push.go).
const NAR_MEDIA_TYPE_PREFIX = "application/vnd.aeroflare.nar.v1+";

const MANIFEST_ACCEPT =
  "application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json";

// narinfo fields, in emission order, as [annotation suffix, canonical field name].
// generateNarinfo loops this table, so adding a field is a one-line change and
// every listed field appears automatically once the pusher sets its annotation.
// The canonical names are fixed by Nix (e.g. "URL", "StorePath") and must match
// exactly, which is why this is a mapping rather than a blind passthrough.
const NARINFO_FIELDS = [
  ["storepath", "StorePath"],
  ["url", "URL"],
  ["compression", "Compression"],
  ["filehash", "FileHash"],
  ["filesize", "FileSize"],
  ["narhash", "NarHash"],
  ["narsize", "NarSize"],
  ["references", "References"],
  ["deriver", "Deriver"],
  ["ca", "CA"],
  ["system", "System"],
  ["sig", "Sig"],
];

// --- Edge cache policy (Workers Cache: enabled by `init` and wrangler config) ---
// Workers Cache sits in front of this Worker and honors the Cache-Control we
// return (RFC 9111); without one it falls back to a short heuristic TTL.
// narinfo and NAR responses are keyed by the store hash, so their content never
// changes: cache them for a year. Misses must stay short, or a path pushed after
// the first lookup would be reported missing for a year. The public key can be
// rotated by `configure`, so it gets a short TTL too.
const CACHE_IMMUTABLE = "public, max-age=31536000, immutable"; // 1 year
const CACHE_CACHE_INFO = "public, max-age=86400"; // 1 day
const CACHE_PUBLIC_KEY = "public, max-age=300"; // 5 minutes
const CACHE_INDEX = "public, max-age=3600"; // 1 hour (fallback landing page)
const CACHE_NOT_FOUND = "public, max-age=60"; // 1 minute
const CACHE_ERROR = "no-store";

// notFound returns a 404 that is only briefly cacheable.
function notFound(body = "Not found") {
  return new Response(body, { status: 404, headers: { "Cache-Control": CACHE_NOT_FOUND } });
}

// --- Module-global caches (persist across requests within a Worker isolate) ---

// Bearer token cache. Registry tokens live a few minutes and are identical for
// every request, so we mint one and reuse it for 4.5 minutes instead of a token
// exchange before every manifest and blob fetch. Lazily expired on read.
const TOKEN_TTL_MS = 4.5 * 60 * 1000;
let cachedBearer = null; // { token, expiry }

// Auth challenge cache. Learned once from a 401's WWW-Authenticate header, so
// after the first cold request we mint tokens directly at the known realm and
// never repeat the blind 401 probe. { realm, service, scope }
let cachedChallenge = null;

// Manifest cache. A client typically fetches "<hash>.narinfo" then
// "/nar/<hash>..." back-to-back; both need the same manifest. Caching it briefly
// spares the second request a registry round-trip. Keyed by tag.
const MANIFEST_TTL_MS = 60 * 1000;
const manifestCache = new Map(); // tag -> { manifest, expiry }

// apiBase returns the OCI Distribution API root, e.g. "https://ghcr.io/v2".
// The operator supplies just the registry URL (no registry is hardcoded); the
// "/v2" prefix is part of the spec, so the worker owns it.
function apiBase(env) {
  const registry = (env.NIXCACHE_REGISTRY_URL || "").replace(/\/+$/, "");
  return `${registry}/v2`;
}

// parseChallenge extracts realm/service/scope from a WWW-Authenticate header
// value like: Bearer realm="https://ghcr.io/token",service="ghcr.io",scope="..."
function parseChallenge(header) {
  if (!header || !/^Bearer /i.test(header)) return null;
  const out = {};
  const re = /(\w+)="([^"]*)"/g;
  let m;
  while ((m = re.exec(header)) !== null) {
    out[m[1]] = m[2];
  }
  return out.realm ? out : null;
}

// mintToken performs the token exchange at the realm the challenge points to,
// attaching Basic credentials from NIXCACHE_TOKEN when present (for private
// repos). Works for any registry because the realm/service/scope come straight
// from the registry's own challenge.
async function mintToken(env, challenge) {
  if (!challenge || !challenge.realm) return "";
  const u = new URL(challenge.realm);
  if (challenge.service) u.searchParams.set("service", challenge.service);
  if (challenge.scope) u.searchParams.set("scope", challenge.scope);

  const headers = {};
  if (env.NIXCACHE_TOKEN) {
    const user = env.NIXCACHE_USERNAME || "token";
    headers["Authorization"] = "Basic " + btoa(`${user}:${env.NIXCACHE_TOKEN}`);
  }

  try {
    const res = await fetch(u.toString(), { headers });
    if (res.ok) {
      const data = await res.json();
      return data.token || data.access_token || "";
    }
    console.error(`Token exchange failed at ${u.origin}: ${res.status}`);
  } catch (err) {
    console.error("Token exchange error:", err);
  }
  return "";
}

// currentBearer returns a bearer token to try WITHOUT waiting for a 401:
// a live cached token, else a token minted from a previously-learned challenge,
// else NIXCACHE_TOKEN used verbatim (covers ready-made JWTs / registries that
// accept the raw token as a bearer). May return "" — the caller handles 401.
async function currentBearer(env) {
  if (cachedBearer && Date.now() < cachedBearer.expiry) {
    return cachedBearer.token;
  }
  if (cachedChallenge) {
    const token = await mintToken(env, cachedChallenge);
    if (token) {
      cachedBearer = { token, expiry: Date.now() + TOKEN_TTL_MS };
      return token;
    }
  }
  return env.NIXCACHE_TOKEN || "";
}

// registryFetch performs an authenticated request against the registry, learning
// and caching the auth challenge on a 401 and retrying once with a fresh token.
async function registryFetch(env, url, accept) {
  const build = (token) => {
    const headers = {};
    if (accept) headers["Accept"] = accept;
    if (token) headers["Authorization"] = `Bearer ${token}`;
    return { headers };
  };

  let res = await fetch(url, build(await currentBearer(env)));
  if (res.status !== 401) return res;

  const challenge = parseChallenge(res.headers.get("WWW-Authenticate"));
  if (!challenge) return res;

  const token = await mintToken(env, challenge);
  if (!token) return res;

  cachedChallenge = challenge;
  cachedBearer = { token, expiry: Date.now() + TOKEN_TTL_MS };
  return fetch(url, build(token));
}

// resolveManifest fetches and briefly caches the OCI manifest for a tag,
// returning the parsed manifest or null on miss.
async function resolveManifest(env, tag) {
  const cached = manifestCache.get(tag);
  if (cached && Date.now() < cached.expiry) {
    return cached.manifest;
  }

  const url = `${apiBase(env)}/${env.NIXCACHE_REPO}/manifests/${tag}`;
  try {
    const res = await registryFetch(env, url, MANIFEST_ACCEPT);
    if (res.ok) {
      const manifest = await res.json();
      manifestCache.set(tag, { manifest, expiry: Date.now() + MANIFEST_TTL_MS });
      return manifest;
    }
  } catch (err) {
    console.error(`Error fetching manifest for tag ${tag}:`, err);
  }
  return null;
}

// narAnnotations returns the aeroflare.* annotations map, or null if absent.
function narAnnotations(manifest) {
  if (manifest && manifest.annotations && manifest.annotations["aeroflare.storepath"]) {
    return manifest.annotations;
  }
  return null;
}

// selectNarLayer picks the NAR blob layer: the one whose mediaType begins with
// the aeroflare NAR prefix, falling back to the first layer.
function selectNarLayer(manifest) {
  if (!manifest || !Array.isArray(manifest.layers) || manifest.layers.length === 0) {
    return null;
  }
  const match = manifest.layers.find(
    (l) => typeof l.mediaType === "string" && l.mediaType.startsWith(NAR_MEDIA_TYPE_PREFIX),
  );
  return match || manifest.layers[0];
}

// generateNarinfo reconstructs a narinfo file by looping NARINFO_FIELDS.
// References is always emitted (with its required trailing space — Nix reads a
// field's value at colon+2, so a bare "References:" would swallow the next line);
// every other field is emitted only when its annotation is present.
function generateNarinfo(ann) {
  const lines = [];
  for (const [key, field] of NARINFO_FIELDS) {
    const val = ann[`aeroflare.${key}`];
    if (field === "References") {
      lines.push(val ? `References: ${val}` : "References: ");
    } else if (val) {
      lines.push(`${field}: ${val}`);
    }
  }
  return lines.join("\n") + "\n";
}

// handlePublicKey serves the cache's public signing key from the
// aeroflare.public-key annotation on the "cache-config" manifest (the tag the
// CLI's `configure` command writes).
async function handlePublicKey(env) {
  const manifest = await resolveManifest(env, "cache-config");
  const ann = (manifest && manifest.annotations) || {};
  const key = ann["aeroflare.public-key"] || ann["public-key"];
  if (key) {
    return new Response(key + "\n", {
      headers: { "Content-Type": "text/plain", "Cache-Control": CACHE_PUBLIC_KEY },
    });
  }
  return notFound("No public key configured");
}

async function handleNarinfo(env, path) {
  const tag = path.replace(/^\//, "").replace(/\.narinfo$/, "");
  const manifest = await resolveManifest(env, tag);
  const ann = narAnnotations(manifest);
  if (ann) {
    return new Response(generateNarinfo(ann), {
      headers: { "Content-Type": "text/x-nix-narinfo", "Cache-Control": CACHE_IMMUTABLE },
    });
  }
  return notFound();
}

async function handleNar(env, path) {
  const basename = path.replace(/^\/nar\//, "");
  const ct = basename.endsWith(".xz") ? "application/x-xz" : "application/x-nix-nar";

  // The manifest tag is the store hash: strip the compression extension from the
  // NAR basename (e.g. <hash>.nar.zst -> <hash>).
  const narIdx = basename.indexOf(".nar");
  const tag = narIdx !== -1 ? basename.slice(0, narIdx) : basename;
  if (!tag) return notFound();

  const manifest = await resolveManifest(env, tag);
  const layer = selectNarLayer(manifest);
  if (layer && layer.digest) {
    const url = `${apiBase(env)}/${env.NIXCACHE_REPO}/blobs/${layer.digest}`;
    try {
      const res = await registryFetch(env, url, null);
      if (res.ok) {
        const headers = { "Content-Type": ct, "Cache-Control": CACHE_IMMUTABLE };
        const len = res.headers.get("Content-Length");
        if (len) headers["Content-Length"] = len;
        return new Response(res.body, { headers });
      }
      console.error(`Blob fetch failed for ${layer.digest}: ${res.status}`);
    } catch (err) {
      console.error(`Failed to fetch blob ${layer.digest}:`, err);
    }
  }

  return notFound();
}

// handleIndex serves the built-in landing page. Normally "/" is answered by the
// static asset public/index.html before the Worker runs (that is the copy users
// edit); this fallback covers deployments without assets, e.g. older CLIs that
// upload worker.js alone. It is public/index.html with the host filled in
// server-side and the logo loaded from the docs site; keep the two in sync.
function handleIndex(url) {
  const host = url.host.replace(/[<>&"]/g, "");
  const html = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Nix binary cache</title>
<link rel="icon" href="https://docs.itzemoji.com/aeroflare/img/logo.svg" type="image/svg+xml">
<script>
  // Apply a saved Light/Dark choice before first paint; none means System.
  try {
    var t = localStorage.getItem("theme");
    if (t === "light" || t === "dark") document.documentElement.dataset.theme = t;
  } catch (e) {}
</script>
<style>
  :root {
    --bg: #ffffff;
    --fg: #1d2327;
    --muted: #5f6b73;
    --line: #e3e8eb;
    --panel: #f6f8f9;
    --accent: #0f7b8a;
    color-scheme: light;
  }
  :root[data-theme="dark"] {
    --bg: #111618;
    --fg: #e4e9ec;
    --muted: #96a3ab;
    --line: #263035;
    --panel: #182024;
    --accent: #6ad7e5;
    color-scheme: dark;
  }
  @media (prefers-color-scheme: dark) {
    :root:not([data-theme="light"]) {
      --bg: #111618;
      --fg: #e4e9ec;
      --muted: #96a3ab;
      --line: #263035;
      --panel: #182024;
      --accent: #6ad7e5;
      color-scheme: dark;
    }
  }
  .theme {
    position: absolute; top: 1rem; right: 1rem;
    font: inherit; font-size: 0.85rem; color: var(--muted);
    background: transparent; border: 1px solid var(--line); border-radius: 6px;
    padding: 0.25rem 0.6rem; cursor: pointer;
  }
  .theme:hover { color: var(--fg); }
  .theme:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
  * { box-sizing: border-box; }
  body {
    margin: 0;
    background: var(--bg);
    color: var(--fg);
    font: 16px/1.6 system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
  }
  main { max-width: 40rem; margin: 0 auto; padding: 4rem 1.25rem 2rem; }
  header { text-align: center; margin-bottom: 2.5rem; }
  header img { width: 140px; height: 140px; }
  h1 { font-size: 1.75rem; margin: 0.5rem 0 0.75rem; letter-spacing: -0.01em; }
  h2 { font-size: 1.05rem; margin: 2.5rem 0 0.75rem; }
  p { margin: 0 0 1rem; }
  .lead { color: var(--muted); font-size: 1.05rem; }
  a { color: var(--accent); text-underline-offset: 2px; }
  code { font: 0.9em ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
  .lead code { color: var(--fg); }
  ul.api { list-style: none; margin: 0; padding: 0; border: 1px solid var(--line); border-radius: 8px; overflow: hidden; }
  ul.api li { display: flex; flex-wrap: wrap; align-items: baseline; gap: 0.25rem 1rem; justify-content: space-between; padding: 0.7rem 1rem; }
  ul.api li + li { border-top: 1px solid var(--line); }
  ul.api li:nth-child(odd) { background: var(--panel); }
  ul.api span { color: var(--muted); font-size: 0.95rem; }
  footer { max-width: 40rem; margin: 0 auto; padding: 1.5rem 1.25rem 3rem; border-top: 1px solid var(--line); color: var(--muted); font-size: 0.9rem; text-align: center; }
  footer a { color: inherit; }
</style>
</head>
<body>
<button class="theme" type="button" title="Switch between system, light and dark theme">Theme: System</button>
<main>
  <header>
    <a href="https://github.com/ItzEmoji/aeroflare"><img src="https://docs.itzemoji.com/aeroflare/img/logo.svg" alt="aeroflare logo"></a>
    <h1>Nix binary cache</h1>
    <p class="lead"><code>https://${host}/</code> provides prebuilt
    binaries for Nix. Once it is added as a substituter, Nix downloads from it
    automatically instead of building locally.</p>
  </header>

  <h2>API</h2>
  <ul class="api">
    <li><a href="/nix-cache-info"><code>/nix-cache-info</code></a><span>Cache metadata</span></li>
    <li><a href="/api/public-key"><code>/api/public-key</code></a><span>Public key the store paths are signed with</span></li>
    <li><code>/&lt;hash&gt;.narinfo</code><span>Information about one store path</span></li>
    <li><code>/nar/&lt;file&gt;</code><span>The compressed store path</span></li>
  </ul>

  <h2>Using this cache</h2>
  <p>Add this URL as a substituter and trust the key from
  <a href="/api/public-key"><code>/api/public-key</code></a>. The NixOS Wiki shows how
  to do that on NixOS and other systems:
  <a href="https://wiki.nixos.org/wiki/Binary_Cache#Using_a_binary_cache">Binary Cache &rarr; Using a binary cache</a>.</p>
  <p>To run a cache like this one yourself, see the
  <a href="https://docs.itzemoji.com/aeroflare/">aeroflare documentation</a>.</p>
</main>
<footer>
  Served by <a href="https://github.com/ItzEmoji/aeroflare">aeroflare</a>
</footer>
<script>
  for (const el of document.querySelectorAll(".host")) el.textContent = location.host;

  // Theme button: cycles System -> Light -> Dark and remembers the choice.
  (function () {
    var root = document.documentElement;
    var button = document.querySelector(".theme");
    var order = ["system", "light", "dark"];
    var names = { system: "System", light: "Light", dark: "Dark" };
    function current() { return root.dataset.theme || "system"; }
    function show() { button.textContent = "Theme: " + names[current()]; }
    button.addEventListener("click", function () {
      var next = order[(order.indexOf(current()) + 1) % order.length];
      if (next === "system") delete root.dataset.theme; else root.dataset.theme = next;
      try {
        if (next === "system") localStorage.removeItem("theme"); else localStorage.setItem("theme", next);
      } catch (e) {}
      show();
    });
    show();
  })();
</script>
</body>
</html>
`;
  return new Response(html, {
    headers: { "Content-Type": "text/html; charset=utf-8", "Cache-Control": CACHE_INDEX },
  });
}

export default {
  async fetch(request, env, ctx) {
    try {
      const url = new URL(request.url);
      const path = url.pathname.replace(/\/$/, "");

      if (path === "" || path === "/index.html") {
        return handleIndex(url);
      }

      if (path === "/nix-cache-info") {
        return new Response("StoreDir: /nix/store\nWantMassQuery: 1\nPriority: 40\n", {
          headers: { "Content-Type": "text/x-nix-cache-info", "Cache-Control": CACHE_CACHE_INFO },
        });
      }

      if (path === "/api/public-key") {
        return handlePublicKey(env);
      }

      if (path.endsWith(".narinfo")) {
        return handleNarinfo(env, path);
      }

      if (path.startsWith("/nar/")) {
        return handleNar(env, path);
      }

      return notFound();
    } catch (err) {
      console.error("Top level fetch error:", err);
      return new Response("Internal Server Error", {
        status: 500,
        headers: { "Cache-Control": CACHE_ERROR },
      });
    }
  },
};
