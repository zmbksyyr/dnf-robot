package webadmin

import (
	"fmt"
	"net/http"
	"os"
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
	partyCompatFailures     int
	partyCompatFirstFailure time.Time
	partyCompatNextRetry    time.Time
	partyCompatLastError    string
}

func New(cfg *config.SysConfig, robotAddr, webAddr string) *Server {
	if robotAddr == "" {
		robotAddr = fmt.Sprintf("127.0.0.1:%d", cfg.RobotPort)
	}
	if webAddr == "" {
		webAddr = fmt.Sprintf("0.0.0.0:%d", cfg.WebPort)
	}
	return &Server{
		cfg:             cfg,
		robotAddr:       robotAddr,
		webAddr:         webAddr,
		tokens:          make(map[string]time.Time),
		partyCompatWake: make(chan struct{}, 1),
	}
}

func (s *Server) ListenAndServe() error {
	stopPartyCompat := s.startPartyCompatSupervisor()
	defer stopPartyCompat()
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
	return server.ListenAndServe()
}
