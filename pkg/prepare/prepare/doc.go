// Package prepare turns Nix store paths into the artifacts a binary cache
// serves: a compressed NAR plus its narinfo metadata.
//
// Prepare handles one store path and PrepareBatch handles many, walking each
// path's closure. With Config.CacheURLs set, paths an upstream cache already
// has are skipped, so only what is genuinely missing gets built; with
// PrepareMissingRefs, the references that are missing upstream are prepared
// too, which is what keeps a pushed closure complete.
//
// A Result names the generated .nar and .narinfo files on disk and, for a
// closure, the Results of its missing references.
package prepare
