package runtimetune

import (
	"errors"
	"testing"
)

func files(m map[string]string) func(string) ([]byte, error) {
	return func(p string) ([]byte, error) {
		if v, ok := m[p]; ok {
			return []byte(v), nil
		}
		return nil, errors.New("no such file")
	}
}

func TestContainerLimit(t *testing.T) {
	v2, v1 := cgroupFiles[0], cgroupFiles[1]
	for name, tc := range map[string]struct {
		files map[string]string
		want  int64
	}{
		"v2 limit":        {map[string]string{v2: "1073741824\n"}, 1 << 30},
		"v2 unlimited":    {map[string]string{v2: "max\n"}, 0},
		"v1 limit":        {map[string]string{v1: "536870912\n"}, 512 << 20},
		"v1 unlimited":    {map[string]string{v1: "9223372036854771712\n"}, 0},
		"v2 wins over v1": {map[string]string{v2: "1073741824", v1: "1"}, 1 << 30},
		"no cgroup":       {map[string]string{}, 0},
		"garbage":         {map[string]string{v2: "lots"}, 0},
		"zero":            {map[string]string{v2: "0"}, 0},
	} {
		if got := containerLimit(files(tc.files)); got != tc.want {
			t.Errorf("%s: got %d, want %d", name, got, tc.want)
		}
	}
}

func TestLimitForAndRatio(t *testing.T) {
	if got := limitFor(1000, 0.8); got != 800 {
		t.Fatalf("got %d", got)
	}
	for _, r := range []float64{0, -1, 1, 2} {
		if limitFor(1000, r) != 0 {
			t.Fatalf("ratio %v must not set a limit", r)
		}
	}
	if limitFor(0, 0.8) != 0 {
		t.Fatal("no container limit, no soft limit")
	}
	for in, want := range map[string]float64{"": 0.8, "abc": 0.8, "0.5": 0.5, " 0.9 ": 0.9, "0": 0, "1.5": 0.8, "-1": 0.8} {
		if got := ratioFromEnv(in); got != want {
			t.Errorf("ratioFromEnv(%q) = %v, want %v", in, got, want)
		}
	}
}

func run(env map[string]string, cg map[string]string) (set int64, logged bool) {
	apply(func(k string) string { return env[k] }, files(cg), func(n int64) int64 { set = n; return 0 }, func(string, ...interface{}) { logged = true })
	return set, logged
}

func TestApply(t *testing.T) {
	cg := map[string]string{cgroupFiles[0]: "1073741824"}

	if set, logged := run(nil, cg); set != limitFor(1<<30, 0.8) || set == 0 || !logged {
		t.Fatalf("with a container limit: %d %v", set, logged)
	}
	if set, _ := run(map[string]string{"MEMORY_LIMIT_RATIO": "0.5"}, cg); set != 512<<20 {
		t.Fatalf("ratio: %d", set)
	}
	if set, logged := run(map[string]string{"GOMEMLIMIT": "300MiB"}, cg); set != 0 || logged {
		t.Fatalf("GOMEMLIMIT is the operator's and is left alone: %d %v", set, logged)
	}
	if set, _ := run(map[string]string{"MEMORY_LIMIT_RATIO": "0"}, cg); set != 0 {
		t.Fatal("MEMORY_LIMIT_RATIO=0 turns it off")
	}
	if set, _ := run(nil, map[string]string{}); set != 0 {
		t.Fatal("without a container limit nothing is set")
	}
}
