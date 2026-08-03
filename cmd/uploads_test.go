package cmd

import (
	"io"
	"os"
	"testing"

	"github.com/axilioai/cli/internal/output"
	platformgo "github.com/axilioai/platform-go"
)

func TestPrintDeliveryWithoutWaitExplainsFutureUsage(t *testing.T) {
	original := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	t.Cleanup(func() {
		os.Stderr = original
		_ = r.Close()
		_ = w.Close()
	})

	printDelivery(output.New("table", false, false), &platformgo.FileDeliverySummary{
		ID:       "delivery-1",
		Filename: "photo.jpg",
		Status:   platformgo.FileDeliverySummaryStatusDispatched,
	}, false)

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stderr = original
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}

	want := "pushed without requesting delivery receipt. In the future, add --wait if you want the cli to wait for delivery confirmation and report result.\n"
	if string(got) != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}
