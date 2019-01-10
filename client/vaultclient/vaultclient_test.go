package vaultclient

import (
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/nomad/client/config"
	"github.com/hashicorp/nomad/helper/testlog"
	"github.com/hashicorp/nomad/testutil"
	vaultapi "github.com/hashicorp/vault/api"
	"github.com/stretchr/testify/require"
)

func TestVaultClient_TokenRenewals(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	v := testutil.NewTestVault(t)
	defer v.Stop()

	logger := testlog.HCLogger(t)
	v.Config.ConnectionRetryIntv = 100 * time.Millisecond
	v.Config.TaskTokenTTL = "4s"
	c, err := NewVaultClient(v.Config, logger, nil)
	if err != nil {
		t.Fatalf("failed to build vault client: %v", err)
	}

	c.Start()
	defer c.Stop()

	// Sleep a little while to ensure that the renewal loop is active
	time.Sleep(time.Duration(testutil.TestMultiplier()) * time.Second)

	tcr := &vaultapi.TokenCreateRequest{
		Policies:    []string{"foo", "bar"},
		TTL:         "2s",
		DisplayName: "derived-for-task",
		Renewable:   new(bool),
	}
	*tcr.Renewable = true

	num := 5
	tokens := make([]string, num)
	for i := 0; i < num; i++ {
		c.client.SetToken(v.Config.Token)

		if err := c.client.SetAddress(v.Config.Addr); err != nil {
			t.Fatal(err)
		}

		secret, err := c.client.Auth().Token().Create(tcr)
		if err != nil {
			t.Fatalf("failed to create vault token: %v", err)
		}

		if secret == nil || secret.Auth == nil || secret.Auth.ClientToken == "" {
			t.Fatal("failed to derive a wrapped vault token")
		}

		tokens[i] = secret.Auth.ClientToken

		errCh, err := c.RenewToken(tokens[i], secret.Auth.LeaseDuration)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		go func(errCh <-chan error) {
			for {
				select {
				case err := <-errCh:
					require.NoError(err, "unexpected error while renewing vault token")
				}
			}
		}(errCh)
	}

	if c.heap.Length() != num {
		t.Fatalf("bad: heap length: expected: %d, actual: %d", num, c.heap.Length())
	}

	time.Sleep(time.Duration(testutil.TestMultiplier()) * time.Second)

	for i := 0; i < num; i++ {
		if err := c.StopRenewToken(tokens[i]); err != nil {
			require.NoError(err)
		}
	}

	if c.heap.Length() != 0 {
		t.Fatalf("bad: heap length: expected: 0, actual: %d", c.heap.Length())
	}
}

func TestVaultClient_Heap(t *testing.T) {
	t.Parallel()
	tr := true
	conf := config.DefaultConfig()
	conf.VaultConfig.Enabled = &tr
	conf.VaultConfig.Token = "testvaulttoken"
	conf.VaultConfig.TaskTokenTTL = "10s"

	logger := testlog.HCLogger(t)
	c, err := NewVaultClient(conf.VaultConfig, logger, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("failed to create vault client")
	}

	now := time.Now()

	renewalReq1 := &vaultClientRenewalRequest{
		errCh:     make(chan error, 1),
		id:        "id1",
		increment: 10,
	}
	if err := c.heap.Push(renewalReq1, now.Add(50*time.Second)); err != nil {
		t.Fatal(err)
	}
	if !c.isTracked("id1") {
		t.Fatalf("id1 should have been tracked")
	}

	renewalReq2 := &vaultClientRenewalRequest{
		errCh:     make(chan error, 1),
		id:        "id2",
		increment: 10,
	}
	if err := c.heap.Push(renewalReq2, now.Add(40*time.Second)); err != nil {
		t.Fatal(err)
	}
	if !c.isTracked("id2") {
		t.Fatalf("id2 should have been tracked")
	}

	renewalReq3 := &vaultClientRenewalRequest{
		errCh:     make(chan error, 1),
		id:        "id3",
		increment: 10,
	}
	if err := c.heap.Push(renewalReq3, now.Add(60*time.Second)); err != nil {
		t.Fatal(err)
	}
	if !c.isTracked("id3") {
		t.Fatalf("id3 should have been tracked")
	}

	// Reading elements should yield id2, id1 and id3 in order
	req, _ := c.nextRenewal()
	if req != renewalReq2 {
		t.Fatalf("bad: expected: %#v, actual: %#v", renewalReq2, req)
	}
	if err := c.heap.Update(req, now.Add(70*time.Second)); err != nil {
		t.Fatal(err)
	}

	req, _ = c.nextRenewal()
	if req != renewalReq1 {
		t.Fatalf("bad: expected: %#v, actual: %#v", renewalReq1, req)
	}
	if err := c.heap.Update(req, now.Add(80*time.Second)); err != nil {
		t.Fatal(err)
	}

	req, _ = c.nextRenewal()
	if req != renewalReq3 {
		t.Fatalf("bad: expected: %#v, actual: %#v", renewalReq3, req)
	}
	if err := c.heap.Update(req, now.Add(90*time.Second)); err != nil {
		t.Fatal(err)
	}

	if err := c.StopRenewToken("id1"); err != nil {
		t.Fatal(err)
	}

	if err := c.StopRenewToken("id2"); err != nil {
		t.Fatal(err)
	}

	if err := c.StopRenewToken("id3"); err != nil {
		t.Fatal(err)
	}

	if c.isTracked("id1") {
		t.Fatalf("id1 should not have been tracked")
	}

	if c.isTracked("id1") {
		t.Fatalf("id1 should not have been tracked")
	}

	if c.isTracked("id1") {
		t.Fatalf("id1 should not have been tracked")
	}

}

func TestVaultClient_RenewNonRenewableLease(t *testing.T) {
	t.Parallel()
	v := testutil.NewTestVault(t)
	defer v.Stop()

	logger := testlog.HCLogger(t)
	v.Config.ConnectionRetryIntv = 100 * time.Millisecond
	v.Config.TaskTokenTTL = "4s"
	c, err := NewVaultClient(v.Config, logger, nil)
	if err != nil {
		t.Fatalf("failed to build vault client: %v", err)
	}

	c.Start()
	defer c.Stop()

	// Sleep a little while to ensure that the renewal loop is active
	time.Sleep(time.Duration(testutil.TestMultiplier()) * time.Second)

	tcr := &vaultapi.TokenCreateRequest{
		Policies:    []string{"foo", "bar"},
		TTL:         "2s",
		DisplayName: "derived-for-task",
		Renewable:   new(bool),
	}

	c.client.SetToken(v.Config.Token)

	if err := c.client.SetAddress(v.Config.Addr); err != nil {
		t.Fatal(err)
	}

	secret, err := c.client.Auth().Token().Create(tcr)
	if err != nil {
		t.Fatalf("failed to create vault token: %v", err)
	}

	if secret == nil || secret.Auth == nil || secret.Auth.ClientToken == "" {
		t.Fatal("failed to derive a wrapped vault token")
	}

	_, err = c.RenewToken(secret.Auth.ClientToken, secret.Auth.LeaseDuration)
	if err == nil {
		t.Fatalf("expected error, got nil")
	} else if !strings.Contains(err.Error(), "lease is not renewable") {
		t.Fatalf("expected \"%s\" in error message, got \"%v\"", "lease is not renewable", err)
	}
}

func TestVaultClient_RenewNonexistentLease(t *testing.T) {
	t.Parallel()
	v := testutil.NewTestVault(t)
	defer v.Stop()

	logger := testlog.HCLogger(t)
	v.Config.ConnectionRetryIntv = 100 * time.Millisecond
	v.Config.TaskTokenTTL = "4s"
	c, err := NewVaultClient(v.Config, logger, nil)
	if err != nil {
		t.Fatalf("failed to build vault client: %v", err)
	}

	c.Start()
	defer c.Stop()

	// Sleep a little while to ensure that the renewal loop is active
	time.Sleep(time.Duration(testutil.TestMultiplier()) * time.Second)

	c.client.SetToken(v.Config.Token)

	if err := c.client.SetAddress(v.Config.Addr); err != nil {
		t.Fatal(err)
	}

	_, err = c.RenewToken(c.client.Token(), 10)
	if err == nil {
		t.Fatalf("expected error, got nil")
		// The Vault error message changed between 0.10.2 and 1.0.1
	} else if !strings.Contains(err.Error(), "lease not found") && !strings.Contains(err.Error(), "lease is not renewable") {
		t.Fatalf("expected \"%s\" or \"%s\" in error message, got \"%v\"", "lease not found", "lease is not renewable", err.Error())
	}
}
