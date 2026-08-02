package webadmin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"robot/internal/foundation/config"
	"robot/internal/foundation/lockhub"
)

type Server struct {
	cfg                     *config.SysConfig
	robotAddr               string
	webAddr                 string
	tokenMu                 lockhub.RWLocker
	tokens                  map[string]time.Time
	partyCompatMu           lockhub.Locker
	partyCompatWake         chan struct{}
	mailboxGuardWake        chan struct{}
	mailboxGuardSnapshot    atomic.Pointer[mailboxGuardConfig]
	partyCompatSnapshot     atomic.Pointer[partyCompatConfig]
	partySkillSnapshot      atomic.Pointer[partySkillFileState]
	partyCompatFailures     int
	partyCompatFirstFailure time.Time
	partyCompatNextRetry    time.Time
	partyCompatLastError    string
	serverScriptMu          lockhub.Locker
	serverScript            serverScriptStatus
	serverScriptCancel      func()
	gameMaxUserMu           lockhub.Locker
	gameMaxUser             gameMaxUserCache
}

type partySkillFileState struct {
	enabled bool
}

func New(cfg *config.SysConfig, robotAddr, webAddr string) *Server {
	if robotAddr == "" {
		robotAddr = fmt.Sprintf("127.0.0.1:%d", cfg.RobotPort)
	}
	if webAddr == "" {
		webAddr = fmt.Sprintf("0.0.0.0:%d", cfg.WebPort)
	}
	return &Server{
		cfg:              cfg,
		robotAddr:        robotAddr,
		webAddr:          webAddr,
		tokens:           make(map[string]time.Time),
		partyCompatWake:  make(chan struct{}, 1),
		mailboxGuardWake: make(chan struct{}, 1),
	}
}

func (s *Server) Serve(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	defer s.stopServerScript()
	stopRuntimeFiles := s.startRuntimeFileWatcher()
	defer stopRuntimeFiles()
	stopPartyCompat := s.startPartyCompatSupervisor()
	defer stopPartyCompat()
	stopMailboxGuard := s.startMailboxGuardSupervisor()
	defer stopMailboxGuard()
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/logout", s.handleLogout)
	mux.HandleFunc("/api/call", s.requireAuth(s.handleCall))
	mux.HandleFunc("/api/game-port", s.requireAuth(s.handleGamePort))
	mux.HandleFunc("/api/game-endpoint", s.requireAuth(s.handleGameEndpoint))
	mux.HandleFunc("/api/restart-robot", s.requireAuth(s.handleRestartRobot))
	mux.HandleFunc("/api/max-user", s.requireAuth(s.handleMaxUser))
	mux.HandleFunc("/api/server-script", s.requireAuth(s.handleServerScript))
	mux.HandleFunc("/api/monitor-service", s.requireAuth(s.handleMonitorService))
	mux.HandleFunc("/api/relay-service", s.requireAuth(s.handleRelayService))
	mux.HandleFunc("/api/party-compat", s.requireAuth(s.handlePartyCompat))
	mux.HandleFunc("/api/compat", s.requireAuth(s.handleCompat))
	mux.HandleFunc("/api/diagnostics", s.requireAuth(s.handleDiagnostics))
	mux.HandleFunc("/api/keypair-download", s.requireAuth(s.handleKeypairDownload))
	server := &http.Server{
		Addr:              s.webAddr,
		Handler:           s.withDiagnostics(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	fmt.Printf("[WebAdmin] listening on %s, robot=%s pid=%d sessions=%d\n", s.webAddr, s.robotAddr, os.Getpid(), s.sessionCount())
	serveDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			s.stopServerScript()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := server.Shutdown(shutdownCtx); err != nil {
				fmt.Printf("[WebAdmin] graceful shutdown failed: %v\n", err)
				if closeErr := server.Close(); closeErr != nil {
					fmt.Printf("[WebAdmin] forced close failed: %v\n", closeErr)
				}
			}
		case <-serveDone:
		}
	}()
	err := server.ListenAndServe()
	close(serveDone)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
