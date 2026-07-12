package push

import (
	"testing"

	"github.com/itzemoji/aeroflare/pkg/prepare/prepare"
)

func TestFlattenTasks_DedupsClosure(t *testing.T) {
	glibc := &prepare.Result{StorePath: "/nix/store/glibc"}
	rootA := &prepare.Result{
		StorePath:         "/nix/store/a",
		MissingRefResults: []*prepare.Result{glibc},
	}
	rootB := &prepare.Result{
		StorePath:         "/nix/store/b",
		MissingRefResults: []*prepare.Result{glibc}, // shared dep
	}
	tasks := flattenTasks([]*prepare.Result{rootA, rootB})
	if len(tasks) != 3 {
		t.Fatalf("expected 3 unique tasks (a, b, glibc), got %d", len(tasks))
	}
	roots := 0
	for _, tk := range tasks {
		if tk.isRoot {
			roots++
		}
	}
	if roots != 2 {
		t.Errorf("expected 2 roots, got %d", roots)
	}
}
