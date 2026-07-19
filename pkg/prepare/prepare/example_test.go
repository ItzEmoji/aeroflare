package prepare_test

import (
	"context"
	"fmt"
	"log"

	"github.com/itzemoji/aeroflare/pkg/prepare/compress"
	"github.com/itzemoji/aeroflare/pkg/prepare/prepare"
)

// Turning a store path into the two artifacts a binary cache serves: a
// compressed NAR and its narinfo.
func ExamplePrepare() {
	cfg := &prepare.Config{
		OutputDir:   "/tmp/aeroflare-out",
		Compression: compress.Zstd,
		Workers:     50,
		// With an upstream listed, paths it already serves are skipped, so only
		// what is genuinely missing gets built.
		CacheURLs: []string{"https://cache.nixos.org"},
		// Prepare the closure's references that the upstream is missing too,
		// which is what keeps a pushed closure complete.
		PrepareMissingRefs: true,
	}

	res, err := prepare.Prepare(
		context.Background(),
		"/nix/store/0nlp2xwzavr9dyrsdhcgnq2h4qxsi8bp-hello-2.12.1",
		cfg,
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(res.NarPath, res.NarinfoPath)
	fmt.Printf("%d reference(s) also needed preparing\n", len(res.MissingRefResults))
}
