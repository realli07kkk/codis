// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

import "testing"

func TestParsePitrInfoText(t *testing.T) {
	const text = "# Replication\r\n" +
		"role:master\r\n" +
		"master_host:\r\n" +
		":empty-key-ignored\r\n" +
		"no colon in this line\r\n" +
		"loading:0\r\n" +
		"\r\n"
	got := parsePitrInfoText(text)
	want := map[string]string{
		"role":        "master",
		"master_host": "",
		"loading":     "0",
	}
	if len(got) != len(want) {
		t.Fatalf("parsePitrInfoText() = %v, want %v", got, want)
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("parsePitrInfoText()[%q] = %q, want %q (all = %v)", key, got[key], value, got)
		}
	}
}
