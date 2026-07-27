package main

import (
	"testing"

	"robot/internal/entry/tcpapi"
)

func TestRequiresValidKeypair(t *testing.T) {
	blocked := []string{
		"createRobots",
		"robotsOnline",
		"robotsOnlineAsync",
		"robotsMove",
		"robotsShout",
		"robotsShoutWorld",
		"robotsShoutLocal",
		"robotsStore",
		"robotsStoreAsync",
		"robotsLogout",
		"robotsLogoutAsync",
		"autoStart",
	}
	for _, cmd := range blocked {
		if !tcpapi.RequiresValidKeypair(cmd) {
			t.Fatalf("expected %s to require a valid keypair", cmd)
		}
	}

	allowed := []string{
		"05",
		"sys",
		"robotsStatus",
		"autoStatus",
		"schedulerStatus",
		"systemStatus",
		"systemAnnouncement",
		"goroutineDump",
		"keypairStatus",
		"keypairReleaseDefault",
		"autoStop",
		"robotConfigGet",
		"robotConfigUpdate",
		"cleanupRobots",
		"cleanupRobotsAsync",
	}
	for _, cmd := range allowed {
		if tcpapi.RequiresValidKeypair(cmd) {
			t.Fatalf("expected %s to be allowed without a valid keypair", cmd)
		}
	}
}
