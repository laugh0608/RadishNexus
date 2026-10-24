//go:build integration

// Package pgfault injects protocol faults into disposable PostgreSQL tests.
package pgfault

import (
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// LoseCommit forwards one connection until PostgreSQL has acknowledged a real
// COMMIT, then closes both sockets without delivering that acknowledgement.
// The returned config is only for a local, non-TLS disposable test database.
// finish must run after the caller has closed its connection or pool.
func LoseCommit(t *testing.T, source *pgx.ConnConfig) (*pgx.ConnConfig, func()) {
	t.Helper()
	if source.TLSConfig != nil || len(source.Fallbacks) != 0 {
		t.Fatal("commit fault requires an explicitly non-TLS test connection")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	dropped := make(chan struct{}, 1)
	upstream := net.JoinHostPort(source.Host, strconv.Itoa(int(source.Port)))
	wg.Go(func() {
		client, err := listener.Accept()
		if err != nil {
			return
		}
		defer client.Close()
		server, err := net.DialTimeout("tcp", upstream, time.Second)
		if err != nil {
			return
		}
		defer server.Close()
		_ = client.SetDeadline(time.Now().Add(15 * time.Second))
		_ = server.SetDeadline(time.Now().Add(15 * time.Second))
		copyDone := make(chan struct{})
		go func() { _, _ = io.Copy(server, client); close(copyDone) }()
		defer func() { client.Close(); server.Close(); <-copyDone }()
		for {
			header := make([]byte, 5)
			if _, err = io.ReadFull(server, header); err != nil {
				return
			}
			size := binary.BigEndian.Uint32(header[1:])
			if size < 4 || size > 1<<20 {
				return
			}
			body := make([]byte, size-4)
			if _, err = io.ReadFull(server, body); err != nil {
				return
			}
			if header[0] == 'C' && string(body) == "COMMIT\x00" {
				dropped <- struct{}{}
				return
			}
			if _, err = client.Write(append(header, body...)); err != nil {
				return
			}
		}
	})
	config := source.Copy()
	config.Host = "127.0.0.1"
	config.Port = uint16(listener.Addr().(*net.TCPAddr).Port)
	return config, func() {
		listener.Close()
		wg.Wait()
		select {
		case <-dropped:
		default:
			t.Error("proxy did not drop a real COMMIT acknowledgement")
		}
	}
}
