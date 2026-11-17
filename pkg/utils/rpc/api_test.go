// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package rpc

import "testing"

func TestRedactRPCURL(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{
			"http://127.0.0.1:8080/api/proxy/0123456789abcdef/stats/1",
			"http://127.0.0.1:8080/api/proxy/:xauth/stats/1",
		},
		{
			"http://127.0.0.1:8080/api/topom/0123456789abcdef/slots",
			"http://127.0.0.1:8080/api/topom/:xauth/slots",
		},
		{
			"http://127.0.0.1:8080/api/dashboard/overview",
			"http://127.0.0.1:8080/api/dashboard/overview",
		},
	}
	for _, c := range cases {
		if got := redactRPCURL(c.in); got != c.want {
			t.Errorf("redactRPCURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
