package notify

import (
	"context"
	"testing"
	"time"
)

func TestSecuritySMTPDestinationAndCancellation(t *testing.T) {
	for _, host := range []string{"localhost", "127.0.0.1", "metadata.internal", "smtp.example.test:25", "smtp.example.test\r\n"} {
		if AllowedSMTPHost(host, []string{"smtp.example.test"}) {
			t.Fatalf("unauthorized host %q", host)
		}
	}
	if !AllowedSMTPHost("smtp.example.test", []string{"smtp.example.test"}) {
		t.Fatal("operator relay refused")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if err := sendTLSMail(ctx, "smtp.example.test", 587, nil, "from@test", []string{"to@test"}, nil); err == nil {
		t.Fatal("cancelled send succeeded")
	}
	if time.Since(start) > time.Second {
		t.Fatal("cancelled SMTP blocked")
	}
	if err := sendTLSMail(context.Background(), "127.0.0.1", 587, nil, "from@test", []string{"to@test"}, nil); err == nil {
		t.Fatal("loopback relay accepted")
	}
}
