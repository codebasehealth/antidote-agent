package logmonitor

import (
	"testing"

	"github.com/codebasehealth/antidote-agent/internal/messages"
)

func TestNewConfigFromMessage(t *testing.T) {
	msg := messages.MonitoringAppConfig{
		RepoFullName:  "owner/repo",
		Framework:     "laravel",
		LogPaths:      []string{"storage/logs/laravel.log"},
		ErrorPatterns: []string{"ERROR", "Exception"},
		ContextLines:  15,
	}

	config := NewConfigFromMessage(msg)

	if config.RepoFullName != "owner/repo" {
		t.Errorf("expected repo 'owner/repo', got '%s'", config.RepoFullName)
	}
	if config.Framework != "laravel" {
		t.Errorf("expected framework 'laravel', got '%s'", config.Framework)
	}
	if len(config.LogPaths) != 1 || config.LogPaths[0] != "storage/logs/laravel.log" {
		t.Errorf("unexpected log paths: %v", config.LogPaths)
	}
	if config.ContextLines != 15 {
		t.Errorf("expected context lines 15, got %d", config.ContextLines)
	}
}

func TestNewConfigFromMessageDefaultContextLines(t *testing.T) {
	msg := messages.MonitoringAppConfig{
		RepoFullName:  "owner/repo",
		ContextLines:  0, // Not set
	}

	config := NewConfigFromMessage(msg)

	if config.ContextLines != 20 {
		t.Errorf("expected default context lines 20, got %d", config.ContextLines)
	}
}

func TestConfigStoreUpdateFromMessage(t *testing.T) {
	store := NewConfigStore()

	msg := &messages.MonitoringConfigMessage{
		Apps: []messages.MonitoringAppConfig{
			{
				RepoFullName:  "owner/app1",
				Framework:     "laravel",
				LogPaths:      []string{"storage/logs/laravel.log"},
				ErrorPatterns: []string{"ERROR"},
				ContextLines:  10,
			},
			{
				RepoFullName:  "owner/app2",
				Framework:     "rails",
				LogPaths:      []string{"log/production.log"},
				ErrorPatterns: []string{"FATAL"},
				ContextLines:  20,
			},
		},
	}

	store.UpdateFromMessage(msg)

	all := store.GetAll()
	if len(all) != 2 {
		t.Fatalf("expected 2 configs, got %d", len(all))
	}

	config1 := store.GetByRepoFullName("owner/app1")
	if config1 == nil {
		t.Fatal("expected to find config for owner/app1")
	}
	if config1.Framework != "laravel" {
		t.Errorf("expected framework 'laravel', got '%s'", config1.Framework)
	}

	config2 := store.GetByRepoFullName("owner/app2")
	if config2 == nil {
		t.Fatal("expected to find config for owner/app2")
	}
	if config2.Framework != "rails" {
		t.Errorf("expected framework 'rails', got '%s'", config2.Framework)
	}
}

func TestConfigStoreSetAppPath(t *testing.T) {
	store := NewConfigStore()

	msg := &messages.MonitoringConfigMessage{
		Apps: []messages.MonitoringAppConfig{
			{
				RepoFullName: "owner/app1",
			},
		},
	}

	store.UpdateFromMessage(msg)

	// Set app path
	store.SetAppPath("owner/app1", "/home/forge/app1")

	config := store.GetByRepoFullName("owner/app1")
	if config.AppPath != "/home/forge/app1" {
		t.Errorf("expected app path '/home/forge/app1', got '%s'", config.AppPath)
	}
}

func TestConfigStoreGetConfigured(t *testing.T) {
	store := NewConfigStore()

	msg := &messages.MonitoringConfigMessage{
		Apps: []messages.MonitoringAppConfig{
			{RepoFullName: "owner/app1"},
			{RepoFullName: "owner/app2"},
			{RepoFullName: "owner/app3"},
		},
	}

	store.UpdateFromMessage(msg)

	// Only set path for app1 and app3
	store.SetAppPath("owner/app1", "/home/forge/app1")
	store.SetAppPath("owner/app3", "/home/forge/app3")

	configured := store.GetConfigured()
	if len(configured) != 2 {
		t.Fatalf("expected 2 configured apps, got %d", len(configured))
	}

	// Verify the configured apps have paths set
	for _, cfg := range configured {
		if cfg.AppPath == "" {
			t.Error("configured app should have app path set")
		}
	}
}

func TestConfigStoreUpdateClearsOld(t *testing.T) {
	store := NewConfigStore()

	// First update
	msg1 := &messages.MonitoringConfigMessage{
		Apps: []messages.MonitoringAppConfig{
			{RepoFullName: "owner/app1"},
			{RepoFullName: "owner/app2"},
		},
	}
	store.UpdateFromMessage(msg1)

	// Second update with different apps
	msg2 := &messages.MonitoringConfigMessage{
		Apps: []messages.MonitoringAppConfig{
			{RepoFullName: "owner/app3"},
		},
	}
	store.UpdateFromMessage(msg2)

	// Old configs should be gone
	if store.GetByRepoFullName("owner/app1") != nil {
		t.Error("expected app1 to be cleared")
	}
	if store.GetByRepoFullName("owner/app2") != nil {
		t.Error("expected app2 to be cleared")
	}

	// New config should exist
	if store.GetByRepoFullName("owner/app3") == nil {
		t.Error("expected app3 to exist")
	}

	if len(store.GetAll()) != 1 {
		t.Errorf("expected 1 config after update, got %d", len(store.GetAll()))
	}
}
