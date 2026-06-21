
async function getOciToken(env) {
  const repo = env.NIXCACHE_REPO || DEFAULT_REPO;
  const registry = env.NIXCACHE_REGISTRY || DEFAULT_REGISTRY;
  
  const scope = `repository:${repo}/nix-cache:pull`;
  const url = `https://${registry}/token?scope=${scope}&service=${registry}`;
  
  try {
    const response = await fetch(url);
    if (response.ok) {
      const data = await response.json();
      return data.token || "";
    }
  } catch (err) {
    console.error("Failed to fetch OCI token:", err);
  }
  return "";
}

async function getIndex(env, ctx) {
  const cache = caches.default;
  const cacheKey = new Request("https://internal.cache/cache-index.json");
  const ttl = parseInt(env.NIXCACHE_INDEX_TTL || DEFAULT_INDEX_TTL);
  
  try {
    let response = await cache.match(cacheKey);
    if (response) {
      return await response.json();
    }
  } catch (err) {
    console.error("Cache match error:", err);
  }

  try {
    // Fetch new index
    const registry = env.NIXCACHE_REGISTRY || DEFAULT_REGISTRY;
    const repo = env.NIXCACHE_REPO || DEFAULT_REPO;
    const token = await getOciToken(env);
    const headers = {
      "Accept": "application/vnd.oci.image.manifest.v1+json"
    };
    if (token) {
      headers["Authorization"] = `Bearer ${token}`;
    }

    const manifestUrl = `https://${registry}/v2/${repo}/nix-cache/manifests/cache-index`;
    const manifestRes = await fetch(manifestUrl, { headers });
    if (!manifestRes.ok) return { entries: {}, gc_roots: [] };
    
    const manifest = await manifestRes.json();
    const layers = manifest.layers || [];
    if (layers.length === 0) return { entries: {}, gc_roots: [] };
    
    const digest = layers[0].digest;
    const blobUrl = `https://${registry}/v2/${repo}/nix-cache/blobs/${digest}`;
    const blobRes = await fetch(blobUrl, { headers });
    if (!blobRes.ok) return { entries: {}, gc_roots: [] };
    
    const indexDataText = await blobRes.text();
    
    // Cache it
    try {
      const cacheResponse = new Response(indexDataText, {
        headers: {
          "Content-Type": "application/json",
          "Cache-Control": `s-maxage=${ttl}`
        }
      });
      ctx.waitUntil(cache.put(cacheKey, cacheResponse));
    } catch (err) {
      console.error("Cache put error:", err);
    }
    
    return JSON.parse(indexDataText);
  } catch (err) {
    console.error("Error fetching index from GHCR:", err);
    return { entries: {}, gc_roots: [] };
  }
}

async function getOciManifestText(env, ctx) {
  const cache = caches.default;
  const cacheKey = new Request("https://internal.cache/oci-manifest.json");
  const ttl = parseInt(env.NIXCACHE_INDEX_TTL || DEFAULT_INDEX_TTL);
  
  try {
    let response = await cache.match(cacheKey);
    if (response) {
      return await response.text();
    }
  } catch (err) {}

  try {
    const registry = env.NIXCACHE_REGISTRY || DEFAULT_REGISTRY;
    const repo = env.NIXCACHE_REPO || DEFAULT_REPO;
    const token = await getOciToken(env);
    const headers = {
      "Accept": "application/vnd.oci.image.manifest.v1+json"
    };
    if (token) {
      headers["Authorization"] = `Bearer ${token}`;
    }

    const manifestUrl = `https://${registry}/v2/${repo}/nix-cache/manifests/cache-index`;
    const manifestRes = await fetch(manifestUrl, { headers });
    if (!manifestRes.ok) return "";
    
    const manifestText = await manifestRes.text();
    
    try {
      const cacheResponse = new Response(manifestText, {
        headers: {
          "Content-Type": "application/json",
          "Cache-Control": `s-maxage=${ttl}`
        }
      });
      ctx.waitUntil(cache.put(cacheKey, cacheResponse));
    } catch (err) {}
    
    return manifestText;
  } catch (err) {
    console.error("Error fetching manifest from GHCR:", err);
    return "";
  }
}

function findNarDigest(index, narBasename) {
  for (const entry of Object.values(index.entries || {})) {
    const narinfo = entry.narinfo || "";
    for (const line of narinfo.split("\n")) {
      if (line.startsWith("URL: ") && line.includes(narBasename)) {
        return entry.nar_digest;
      }
    }
  }
  return null;
}

export default {
  async fetch(request, env, ctx) {
    try {
      const url = new URL(request.url);
      const path = url.pathname.replace(/\/$/, "");
      
      const upstream = env.NIXCACHE_UPSTREAM ? env.NIXCACHE_UPSTREAM.split(" ") : DEFAULT_UPSTREAM;
      const registry = env.NIXCACHE_REGISTRY || DEFAULT_REGISTRY;
      const repo = env.NIXCACHE_REPO || DEFAULT_REPO;

      if (path === "/nix-cache-info") {
        const info = "StoreDir: /nix/store\nWantMassQuery: 1\nPriority: 40\n";
        return new Response(info, {
          headers: { "Content-Type": "text/x-nix-cache-info" }
        });
      }

      if (path === "/api/public-key") {
        const manifestText = await getOciManifestText(env, ctx);
        const match = manifestText.match(/"public[-_]key"\s*:\s*"([^"]+)"/);
        
        if (match && match[1]) {
          return new Response(match[1] + "\n", { headers: { "Content-Type": "text/plain" }});
        }
        
        return new Response("No public key configured in manifest", { status: 404 });
      }

      if (path === "/public-key") {
        const index = await getIndex(env, ctx);
        if (index.public_key) {
          return new Response(index.public_key + "\n", { headers: { "Content-Type": "text/plain" }});
        }
        return new Response("No public key configured", { status: 404 });
      }

      if (path === "/_status") {
        const index = await getIndex(env, ctx);
        const status = {
          index_entries: Object.keys(index.entries || {}).length,
          index_generated: index.generated || "unknown",
          index_ttl: parseInt(env.NIXCACHE_INDEX_TTL || DEFAULT_INDEX_TTL),
          repo: repo,
          upstream: upstream
        };
        return new Response(JSON.stringify(status, null, 2) + "\n", {
          headers: { "Content-Type": "application/json" }
        });
      }

      if (path === "/_index") {
        const index = await getIndex(env, ctx);
        return new Response(JSON.stringify(index.entries || {}, null, 2) + "\n", {
          headers: { "Content-Type": "application/json" }
        });
      }

      if (request.method === "POST" && path === "/_refresh") {
        try {
          const cache = caches.default;
          await cache.delete(new Request("https://internal.cache/cache-index.json"));
        } catch (e) { }
        const index = await getIndex(env, ctx);
        const count = Object.keys(index.entries || {}).length;
        return new Response(JSON.stringify({ refreshed: true, entries: count }) + "\n", {
          headers: { "Content-Type": "application/json" }
        });
      }

      if (path.endsWith(".narinfo")) {
        const storeHash = path.replace(/^\//, "").replace(/\.narinfo$/, "");
        const index = await getIndex(env, ctx);
        const entry = (index.entries || {})[storeHash];
        
        if (entry && entry.narinfo) {
          return new Response(entry.narinfo, {
            headers: { "Content-Type": "text/x-nix-narinfo" }
          });
        }

        for (const cacheUrl of upstream) {
          const fetchUrl = `${cacheUrl}/${storeHash}.narinfo`;
          try {
            const res = await fetch(fetchUrl);
            if (res.ok) {
              return new Response(res.body, {
                headers: { "Content-Type": "text/x-nix-narinfo" }
              });
            }
          } catch (err) {
            console.error(`Failed to fetch upstream ${fetchUrl}:`, err);
          }
        }
        return new Response("Not found", { status: 404 });
      }

      if (path.startsWith("/nar/")) {
        const narBasename = path.replace(/^\/nar\//, "");
        const ct = narBasename.endsWith(".xz") ? "application/x-xz" : "application/x-nix-nar";
        
        const index = await getIndex(env, ctx);
        const narDigest = findNarDigest(index, narBasename);
        
        if (narDigest) {
          try {
            const token = await getOciToken(env);
            const headers = {};
            if (token) headers["Authorization"] = `Bearer ${token}`;
            
            const blobUrl = `https://${registry}/v2/${repo}/nix-cache/blobs/${narDigest}`;
            const res = await fetch(blobUrl, { headers });
            if (res.ok) {
              return new Response(res.body, {
                headers: { 
                  "Content-Type": ct, 
                  "Content-Length": res.headers.get("Content-Length") || undefined 
                }
              });
            }
          } catch (err) {
            console.error(`Failed to fetch blob ${narDigest} from GHCR:`, err);
          }
        }

        for (const cacheUrl of upstream) {
          const fetchUrl = `${cacheUrl}${path}`;
          try {
            const res = await fetch(fetchUrl);
            if (res.ok) {
              return new Response(res.body, {
                headers: { 
                  "Content-Type": ct, 
                  "Content-Length": res.headers.get("Content-Length") || undefined 
                }
              });
            }
          } catch (err) {
            console.error(`Failed to fetch upstream ${fetchUrl}:`, err);
          }
        }
        return new Response("Not found", { status: 404 });
      }

      // Fallback to static UI assets
      if (env.ASSETS) {
        return env.ASSETS.fetch(request);
      }
      return new Response("Not found", { status: 404 });
    } catch (err) {
      console.error("Top level fetch error:", err);
      return new Response("Internal Server Error", { status: 500 });
    }
  }
};
