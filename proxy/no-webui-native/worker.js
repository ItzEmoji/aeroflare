let CACHED_IMAGE_PATH = null;

async function getOciToken(env, imagePath) {
  const registry = env.NIXCACHE_REGISTRY || DEFAULT_REGISTRY;
  const scope = `repository:${imagePath}:pull`;
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

async function getOciManifestAndPath(env, ctx, tag) {
  const cache = caches.default;
  const cacheKey = new Request(`https://internal.cache/manifest-v3/${tag}`);
  const ttl = parseInt(env.NIXCACHE_INDEX_TTL || DEFAULT_INDEX_TTL);
  
  try {
    let response = await cache.match(cacheKey);
    if (response) {
      return await response.json();
    }
  } catch (err) {}

  const registry = env.NIXCACHE_REGISTRY || DEFAULT_REGISTRY;
  const repo = env.NIXCACHE_REPO || DEFAULT_REPO;
  
  // The user might be pushing tags directly to repo, or to repo/nix-cache.
  const paths = CACHED_IMAGE_PATH ? [CACHED_IMAGE_PATH] : [repo, `${repo}/nix-cache`];
  
  for (const imagePath of paths) {
    const token = await getOciToken(env, imagePath);
    const headers = {
      "Accept": "application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json"
    };
    if (token) headers["Authorization"] = `Bearer ${token}`;

    const manifestUrl = `https://${registry}/v2/${imagePath}/manifests/${tag}`;
    try {
      const manifestRes = await fetch(manifestUrl, { headers });
      if (manifestRes.ok) {
        CACHED_IMAGE_PATH = imagePath;
        const manifest = await manifestRes.json();
        const result = { manifest, imagePath };
        
        try {
          const cacheResponse = new Response(JSON.stringify(result), {
            headers: {
              "Content-Type": "application/json",
              "Cache-Control": `s-maxage=${ttl}`
            }
          });
          ctx.waitUntil(cache.put(cacheKey, cacheResponse));
        } catch (err) {}
        
        return result;
      }
    } catch (err) {
      console.error(`Error fetching manifest for tag ${tag} at path ${imagePath}:`, err);
    }
  }
  
  return null;
}

async function getNarLabels(env, ctx, manifest, imagePath) {
  if (!manifest) return null;

  // 1. Check annotations (Standard OCI Image Manifest)
  if (manifest.annotations && manifest.annotations["vnd.aeroflare.nar.storepath"]) {
    return manifest.annotations;
  }
  // 2. Check labels at root (Non-standard but sometimes output by CLI tools)
  if (manifest.labels && manifest.labels["vnd.aeroflare.nar.storepath"]) {
    return manifest.labels;
  }

  // 3. Fallback to fetch config blob (Standard Docker Manifest behavior)
  if (manifest.config && manifest.config.digest) {
    const registry = env.NIXCACHE_REGISTRY || DEFAULT_REGISTRY;
    const token = await getOciToken(env, imagePath);
    const headers = {};
    if (token) headers["Authorization"] = `Bearer ${token}`;

    const configUrl = `https://${registry}/v2/${imagePath}/blobs/${manifest.config.digest}`;
    try {
      const configRes = await fetch(configUrl, { headers });
      if (configRes.ok) {
        const config = await configRes.json();
        if (config.config && config.config.Labels) return config.config.Labels;
        if (config.Labels) return config.Labels;
        if (config.config && config.config.labels) return config.config.labels;
        if (config.labels) return config.labels;
      }
    } catch (err) {
      console.error("Failed to fetch config blob:", err);
    }
  }

  return null;
}

function generateNarinfo(labels) {
  const map = {
    "vnd.aeroflare.nar.storepath": "StorePath",
    "vnd.aeroflare.nar.url": "URL",
    "vnd.aeroflare.nar.compression": "Compression",
    "vnd.aeroflare.nar.filehash": "FileHash",
    "vnd.aeroflare.nar.filesize": "FileSize",
    "vnd.aeroflare.nar.narhash": "NarHash",
    "vnd.aeroflare.nar.narsize": "NarSize",
    "vnd.aeroflare.nar.deriver": "Deriver",
    "vnd.aeroflare.nar.system": "System",
    "vnd.aeroflare.nar.sig": "Sig",
    "vnd.aeroflare.nar.references": "References"
  };

  const order = ["StorePath", "URL", "Compression", "FileHash", "FileSize", "NarHash", "NarSize", "Deriver", "System", "Sig"];
  const extracted = {};
  
  for (const [k, v] of Object.entries(labels)) {
    if (map[k]) {
      extracted[map[k]] = v;
    } else if (k.startsWith("vnd.aeroflare.nar.")) {
      const key = k.split('.').pop();
      const title = key.charAt(0).toUpperCase() + key.slice(1);
      extracted[title] = v;
    }
  }

  let lines = [];
  for (const key of order) {
    if (extracted[key] !== undefined) {
      lines.push(`${key}: ${extracted[key]}`);
      delete extracted[key];
    }
  }
  
  for (const [key, value] of Object.entries(extracted)) {
    lines.push(`${key}: ${value}`);
  }
  
  return lines.join("\n") + "\n";
}

export default {
  async fetch(request, env, ctx) {
    try {
      const url = new URL(request.url);
      const path = url.pathname.replace(/\/$/, "");
      
      const upstream = env.NIXCACHE_UPSTREAM ? env.NIXCACHE_UPSTREAM.split(" ") : DEFAULT_UPSTREAM;
      const registry = env.NIXCACHE_REGISTRY || DEFAULT_REGISTRY;

      if (path === "/nix-cache-info") {
        const info = "StoreDir: /nix/store\nWantMassQuery: 1\nPriority: 40\n";
        return new Response(info, {
          headers: { "Content-Type": "text/x-nix-cache-info" }
        });
      }

      if (path === "/api/public-key" || path === "/public-key") {
        if (env.NIXCACHE_PUBLIC_KEY) {
          return new Response(env.NIXCACHE_PUBLIC_KEY + "\n", { headers: { "Content-Type": "text/plain" }});
        }
        
        // Fallback to cache-index manifest
        const result = await getOciManifestAndPath(env, ctx, "cache-index");
        if (result && result.manifest) {
           const labels = await getNarLabels(env, ctx, result.manifest, result.imagePath);
           if (labels && labels["aeroflare.public-key"]) {
             return new Response(labels["aeroflare.public-key"] + "\n", { headers: { "Content-Type": "text/plain" }});
           }
           if (labels && labels["public-key"]) {
             return new Response(labels["public-key"] + "\n", { headers: { "Content-Type": "text/plain" }});
           }
           if (result.manifest.annotations && result.manifest.annotations["aeroflare.public-key"]) {
             return new Response(result.manifest.annotations["aeroflare.public-key"] + "\n", { headers: { "Content-Type": "text/plain" }});
           }
           if (result.manifest.annotations && result.manifest.annotations["public-key"]) {
             return new Response(result.manifest.annotations["public-key"] + "\n", { headers: { "Content-Type": "text/plain" }});
           }
        }
        
        return new Response("No public key configured", { status: 404 });
      }

      if (path.endsWith(".narinfo")) {
        const storeHash = path.replace(/^\//, "").replace(/\.narinfo$/, "");
        
        // Fetch manifest for the storeHash tag
        const result = await getOciManifestAndPath(env, ctx, storeHash);
        if (result && result.manifest) {
          const labels = await getNarLabels(env, ctx, result.manifest, result.imagePath);
          if (labels) {
            const narinfo = generateNarinfo(labels);
            return new Response(narinfo, {
              headers: { "Content-Type": "text/x-nix-narinfo" }
            });
          }
        }

        // Fallback to upstream
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
        const ct = narBasename.endsWith(".xz") ? "application/x-xz" : 
                   narBasename.endsWith(".zst") ? "application/zstd" : "application/x-nix-nar";
        
        // The storeHash is typically the first 32 characters of the nar file name
        const storeHashMatch = narBasename.match(/^([a-z0-9]{32})/);
        
        if (storeHashMatch) {
          const storeHash = storeHashMatch[1];
          const result = await getOciManifestAndPath(env, ctx, storeHash);
          
          if (result && result.manifest && result.manifest.layers && result.manifest.layers.length > 0) {
            // First layer is the nar blob
            const narDigest = result.manifest.layers[0].digest;
            
            try {
              const token = await getOciToken(env, result.imagePath);
              const headers = {};
              if (token) headers["Authorization"] = `Bearer ${token}`;
              
              const blobUrl = `https://${registry}/v2/${result.imagePath}/blobs/${narDigest}`;
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
