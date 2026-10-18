//go:build integration

package postgres_test

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authn"
	authpostgres "github.com/laugh0608/RadishNexus/server/internal/platform/authn/postgres"
)

// A wire proxy discards the actual PostgreSQL COMMIT acknowledgement after the
// server has committed. No mock transaction or synthetic commit error is used.
func TestRecoveryLostCommitAcknowledgement(t *testing.T) {
	f := newRecoveryFixture(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var wg sync.WaitGroup
	dropped := make(chan struct{}, 1)
	upstream := net.JoinHostPort(f.pool.Config().ConnConfig.Host, fmtPort(f.pool.Config().ConnConfig.Port))
	wg.Go(func() {
		client, e := listener.Accept()
		if e != nil {
			return
		}
		defer client.Close()
		server, e := net.DialTimeout("tcp", upstream, time.Second)
		if e != nil {
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
			if _, e = io.ReadFull(server, header); e != nil {
				return
			}
			size := binary.BigEndian.Uint32(header[1:])
			if size < 4 || size > 1<<20 {
				return
			}
			body := make([]byte, size-4)
			if _, e = io.ReadFull(server, body); e != nil {
				return
			}
			if header[0] == 'C' && string(body) == "COMMIT\x00" {
				dropped <- struct{}{}
				return
			}
			if _, e = client.Write(append(header, body...)); e != nil {
				return
			}
		}
	})
	config := f.pool.Config().Copy()
	config.ConnConfig.Host = "127.0.0.1"
	config.ConnConfig.Port = uint16(listener.Addr().(*net.TCPAddr).Port)
	config.ConnConfig.TLSConfig = nil
	config.ConnConfig.Fallbacks = nil
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(f.ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	service := authn.NewRecoveryService(authpostgres.New(pool), authn.NewArgon2idHasher(), authn.CryptoSecretGenerator{})
	input := f.input(t, 1)
	_, err = service.Apply(f.ctx, input)
	pool.Close()
	listener.Close()
	wg.Wait()
	if !errors.Is(err, authn.ErrRecoveryUnconfirmed) {
		t.Fatal("commit loss was not uncertain", err)
	}
	select {
	case <-dropped:
	default:
		t.Fatal("proxy did not drop commit acknowledgement")
	}
	status, err := f.service.Status(f.ctx, input.UserID, input.OperationID)
	if err != nil || status.Result != "applied" {
		t.Fatal("committed operation not found", err)
	}
	if _, err = f.service.Apply(f.ctx, input); !errors.Is(err, authn.ErrRecoveryRecorded) {
		t.Fatal("uncertain operation replayed", err)
	}
	if _, err = f.auth.Login(f.ctx, authn.LoginInput{Email: recoveryEmail, Password: recoveryNewPassword}); err != nil {
		t.Fatal("committed password absent", err)
	}
}
func fmtPort(port uint16) string { return fmt.Sprintf("%d", port) }
