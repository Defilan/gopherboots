package main

import "testing"

// TestHandleBootstrapErrorCommand tests the run
func TestHandleBootstrapErrorCommand(t *testing.T) {
	dnsOut := []byte("nodename nor servname provided")
	authOut := []byte("Authentication failed")
	timeoutOut := []byte("ConnectionTimeout")
	authHost := Host{
		Hostname: "testhost_auth",
		Domain:   "testdomain",
		ChefEnv:  "testenv",
		RunList:  "testrecipe",
	}
	dnsHost := Host{
		Hostname: "testhost_dns",
		Domain:   "testdomain",
		ChefEnv:  "testenv",
		RunList:  "testrecipe",
	}
	timeoutHost := Host{
		Hostname: "testhost_timeout",
		Domain:   "testdomain",
		ChefEnv:  "testenv",
		RunList:  "testrecipe",
	}

	dnsResult := handleBootstrapError(dnsOut, dnsHost, 1)
	if dnsResult != true {
		t.Error("Expected dns error true, got ", dnsResult)
	}
	authResult := handleBootstrapError(authOut, authHost, 1)
	if authResult != true {
		t.Error("Expected auth error true, got ", authResult)
	}
	timeoutResult := handleBootstrapError(timeoutOut, timeoutHost, 1)
	if timeoutResult != true {
		t.Error("Expected timeout error true, got ", timeoutResult)
	}

}
