package config

import "testing"

func TestParseTrustedProxies(t *testing.T) {
	got, err := parseTrustedProxies(" 10.0.0.0/8, 172.18.0.2 ,,::1 ")
	if err != nil || len(got) != 3 || got[0] != "10.0.0.0/8" || got[1] != "172.18.0.2" || got[2] != "::1" {
		t.Fatalf("got %v %v", got, err)
	}
	if got, err := parseTrustedProxies(""); err != nil || got != nil {
		t.Fatalf("empty means no trusted proxy: %v %v", got, err)
	}
	for _, bad := range []string{"proxy.example.com", "10.0.0.0/33", "300.1.1.1"} {
		if _, err := parseTrustedProxies(bad); err == nil {
			t.Fatalf("%q must be refused", bad)
		}
	}
}

func TestEnvNonNegative(t *testing.T) {
	t.Setenv("TEST_NN", "")
	if envNonNegative("TEST_NN", 30) != 30 {
		t.Fatal("unset gives the default")
	}
	t.Setenv("TEST_NN", "0")
	if envNonNegative("TEST_NN", 30) != 0 {
		t.Fatal("0 is a valid value (it turns the limit off)")
	}
	t.Setenv("TEST_NN", "12")
	if envNonNegative("TEST_NN", 30) != 12 {
		t.Fatal("set")
	}
	for _, bad := range []string{"-1", "abc"} {
		t.Setenv("TEST_NN", bad)
		if envNonNegative("TEST_NN", 30) != 30 {
			t.Fatalf("%q gives the default", bad)
		}
	}
}
