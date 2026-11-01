package controller

import (
	"context"
	"net"
	"net/netip"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestDialNodeSourceAddressesFallsBackPastUnreachableAddress(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	blackhole := net.JoinHostPort("127.0.0.2", port)
	dialer := &net.Dialer{Timeout: 5 * time.Second, ControlContext: func(ctx context.Context, network, address string, _ syscall.RawConn) error {
		if address == blackhole {
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}}
	addresses := []netip.Addr{netip.MustParseAddr("127.0.0.2"), netip.MustParseAddr("127.0.0.1")}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := dialNodeSourceAddresses(ctx, dialer, "tcp", addresses, port, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("expected fallback to reachable address, got %v", err)
	}
	defer conn.Close()
	if got := conn.RemoteAddr().String(); got != listener.Addr().String() {
		t.Fatalf("connected to %s, want %s", got, listener.Addr())
	}
}

func TestDialNodeSourceAddressesReportsFailureWhenAllFail(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	listener.Close()
	dialer := &net.Dialer{Timeout: time.Second}
	_, err = dialNodeSourceAddresses(context.Background(), dialer, "tcp", []netip.Addr{netip.MustParseAddr("127.0.0.1")}, port, 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected dial error")
	}
}

func TestInterleaveNodeSourceAddressesAlternatesFamilies(t *testing.T) {
	input := []netip.Addr{
		netip.MustParseAddr("2001:4860::1"),
		netip.MustParseAddr("2001:4860::2"),
		netip.MustParseAddr("8.8.8.8"),
		netip.MustParseAddr("8.8.4.4"),
	}
	got := interleaveNodeSourceAddresses(input)
	want := []string{"2001:4860::1", "8.8.8.8", "2001:4860::2", "8.8.4.4"}
	for i, address := range got {
		if address.String() != want[i] {
			t.Fatalf("order %v, want %v", got, want)
		}
	}
}
