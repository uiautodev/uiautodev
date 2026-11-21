package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"
	"github.com/uiautodev/uiautodev/version"
	"github.com/uiautodev/uiautodev/webserver"
	"github.com/uiautodev/uiautodev/webserver/cluster"

	"github.com/MatusOllah/slogcolor"
	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/log"
	"github.com/pkg/browser"
)

// stringSlice is a custom flag type that allows multiple -auth flags
type stringSlice []string

func (s *stringSlice) String() string {
	return strings.Join(*s, ", ")
}

func (s *stringSlice) Set(value string) error {
	// Validate format: must contain exactly one colon with non-empty username and password
	parts := strings.SplitN(value, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		slog.Warn("invalid auth format", "expected_format", "user:pass")
		return errors.New("wrong format, expect user:pass")
	}
	*s = append(*s, value)
	return nil
}

//go:embed all:frontend_build
var assets embed.FS

func setupLogger(level slog.Level) {
	options := *slogcolor.DefaultOptions
	options.Level = level
	logger := slog.New(slogcolor.NewHandler(os.Stderr, &options))
	slog.SetDefault(logger)
}

func handleStaticSPA(e *echo.Echo, assets fs.FS, subdir string) error {
	var root = assets
	if subdir != "" {
		s, err := fs.Sub(assets, subdir)
		if err != nil {
			return err
		}
		root = s
	}

	// Serve static assets and fallback to index.html for SPA routes
	e.GET("/*", func(c echo.Context) error {
		reqPath := c.Request().URL.Path[1:] // remove leading slash
		file, err := root.Open(reqPath)
		if err != nil {
			// favicon.ico is not present in the build; return 404 to avoid falling back to index.html
			if strings.EqualFold(reqPath, "favicon.ico") {
				c.Response().Header().Set("Cache-Control", "no-cache")
				return c.NoContent(http.StatusNotFound)
			}
			// Fallback to index.html for SPA routes
			file, err = root.Open("index.html")
			if err != nil {
				return c.NoContent(http.StatusNotFound)
			}
			reqPath = "index.html"
		}
		defer file.Close()

		// Set cache headers based on file type
		ext := strings.ToLower(path.Ext(reqPath))
		noCacheExts := map[string]bool{
			".html": true,
			".htm":  true,
			".json": true,
			".xml":  true,
		}

		// File extensions that should be cached long-term
		longTermCacheExts := map[string]bool{
			".js":    true,
			".css":   true,
			".woff":  true,
			".woff2": true,
			".ttf":   true,
			".eot":   true,
			".otf":   true,
		}

		if noCacheExts[ext] {
			// Don't cache HTML files to ensure updates are picked up
			c.Response().Header().Set("Cache-Control", "no-cache")
		} else if longTermCacheExts[ext] {
			// Long-term cache for static assets (1 year)
			c.Response().Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			// Short-term cache for other static files (1 hour)
			c.Response().Header().Set("Cache-Control", "public, max-age=3600")
		}

		// embed.FS files have zero modTime; use build time so ETag changes per version
		buildTime, _ := time.Parse(time.RFC3339, version.BuildTime)
		http.ServeContent(c.Response(), c.Request(), reqPath, buildTime, file.(io.ReadSeeker))
		return nil
	})

	return nil
}

func runSlave(addr string, cfg webserver.ServerConfig, assets fs.FS, masterURL, token, name string, insecureTLS bool) error {
	e := webserver.CreateEchoServerWithConfig(cfg)
	e.Logger.SetLevel(log.DEBUG)

	if strings.TrimSpace(addr) != "" {
		if err := handleStaticSPA(e, assets, "frontend_build"); err != nil {
			return err
		}
		go func() {
			if err := webserver.StartWithConfig(e, addr, cfg); err != nil && err != http.ErrServerClosed {
				slog.Error("slave local server failed", "error", err)
			}
		}()
	}

	agent, err := cluster.NewAgent(cluster.AgentConfig{
		MasterURL:   masterURL,
		Token:       token,
		Name:        name,
		NodeID:      webserver.ClusterNodeID(),
		Version:     version.Version,
		InsecureTLS: insecureTLS,
	}, e)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	agent.Start(ctx)
	slog.Info("slave mode started", "master", masterURL, "name", name, "local_addr", addr)
	<-ctx.Done()
	// Restore default signal handling so a second Ctrl-C still terminates the
	// process if shutdown gets stuck.
	stop()
	agent.Stop()
	return nil
}

func main() {
	addr := flag.String("addr", "localhost:33299", "HTTP listen address")
	openBrowser := flag.Bool("open", false, "open browser after server starts")
	showVersion := flag.Bool("version", false, "print version and exit")
	verbose := flag.Bool("verbose", false, "enable debug level logging")
	disableDeviceListing := flag.Bool("disable-device-listing", false, "disable device listing endpoint for security reasons")
	cors := flag.Bool("cors", false, "enable CORS for cross-origin requests")
	proxyLogPath := flag.String("proxy-log", "", "path to proxy log file (empty to disable logging)")
	configDir := flag.String("config-dir", "", "directory holding config.json and the default plugin dir (default ~/.config/uiautodev); use it to isolate state between multiple servers")
	pluginDir := flag.String("plugin-dir", "", "directory where user plugins are installed and loaded from (default <config-dir>/plugins)")
	var authUsers stringSlice
	flag.Var(&authUsers, "auth", "authentication credentials in format username:password (can be specified multiple times)")
	tlsAddr := flag.String("tls-addr", "", "HTTPS listen address (enables TLS, also starts HTTP redirect on --addr)")
	tlsCertFile := flag.String("tls-cert", "", "path to TLS certificate file (PEM format)")
	tlsKeyFile := flag.String("tls-key", "", "path to TLS private key file (PEM format)")
	enableNgrok := flag.Bool("ngrok", false, "expose the server publicly via an ngrok tunnel")
	ngrokAuthtoken := flag.String("ngrok-authtoken", "", "ngrok authtoken (overrides NGROK_AUTHTOKEN env var)")
	masterURL := flag.String("master-url", "", "upstream master HTTP(S) origin, e.g. https://master.example.com; the node also joins it as a slave but stays a master itself")
	clusterToken := flag.String("token", "", "slave token generated by the upstream master (with -master-url; also UIAUTODEV_CLUSTER_TOKEN)")
	slaveName := flag.String("name", "", "display name reported to the upstream master (defaults to the machine hostname)")
	masterInsecure := flag.Bool("master-insecure", false, "skip TLS verification for the upstream master certificate")
	flag.Parse()
	if *showVersion {
		println("uiautodev-server version:", version.Version)
		return
	}
	if *verbose {
		setupLogger(slog.LevelDebug)
	}

	cfg := webserver.ServerConfig{
		DisableDeviceListing: *disableDeviceListing,
		ProxyLogPath:         *proxyLogPath,
		AuthUsers:            []string(authUsers),
		CORS:                 *cors,
		Audience:             "server",
		TLSAddr:              *tlsAddr,
		TLSCertFile:          *tlsCertFile,
		TLSKeyFile:           *tlsKeyFile,
		StartIOSWDAWatcher:   true,
	}

	// -config-dir and -plugin-dir isolate state between multiple servers.
	// -config-dir is applied first so the default plugin dir follows it.
	if *configDir != "" {
		webserver.SetUserConfigDir(*configDir)
	}
	if *pluginDir != "" {
		webserver.SetUserPluginDir(*pluginDir)
	}

	slaveMode := strings.TrimSpace(*masterURL) != ""
	if !slaveMode {
		if *clusterToken != "" || *slaveName != "" || *masterInsecure || os.Getenv("UIAUTODEV_CLUSTER_TOKEN") != "" {
			slog.Warn("-token, -name and -master-insecure are ignored without -master-url")
		}
	}

	if slaveMode {
		if len(cfg.AuthUsers) > 0 {
			slog.Warn("slave mode ignores -auth; authentication is handled by the master")
			cfg.AuthUsers = nil
		}
		token := *clusterToken
		if token == "" {
			token = os.Getenv("UIAUTODEV_CLUSTER_TOKEN")
		}
		if strings.TrimSpace(token) == "" {
			slog.Error("slave mode requires -token or UIAUTODEV_CLUSTER_TOKEN")
			os.Exit(1)
		}
		slaveAddr, err := cluster.NormalizeSlaveAddr(*addr)
		if err != nil {
			slog.Error("invalid slave listen address", "error", err)
			os.Exit(1)
		}
		if err := runSlave(slaveAddr, cfg, assets, *masterURL, token, *slaveName, *masterInsecure); err != nil {
			slog.Error("slave failed", "error", err)
			os.Exit(1)
		}
		return
	}
	if strings.TrimSpace(*addr) == "" {
		slog.Error("master mode requires a non-empty -addr")
		os.Exit(1)
	}

	e := webserver.CreateEchoServerWithConfig(cfg)
	// mount embedded frontend build located under `frontend_build` in the embed.FS
	if err := handleStaticSPA(e, assets, "frontend_build"); err != nil {
		slog.Error("failed to mount static assets", "err", err)
	}
	e.Logger.SetLevel(log.DEBUG)

	var ngrokTunnel *webserver.Tunnel
	if *enableNgrok {
		authtoken := *ngrokAuthtoken
		if authtoken == "" {
			authtoken = os.Getenv("NGROK_AUTHTOKEN")
		}
		if authtoken == "" {
			slog.Error("ngrok tunnel requires an authtoken (set -ngrok-authtoken or NGROK_AUTHTOKEN env var)")
			os.Exit(1)
		}
		if len(authUsers) == 0 {
			slog.Warn("ngrok tunnel is exposed publicly without auth; consider adding -auth user:pass")
		}

		upstreamAddr := *addr
		if cfg.TLSAddr != "" {
			upstreamAddr = "https://" + cfg.TLSAddr
		}
		t, err := webserver.StartTunnel(context.Background(), webserver.TunnelConfig{
			Upstream:  webserver.UpstreamFromAddr(upstreamAddr),
			Authtoken: authtoken,
		})
		if err != nil {
			slog.Error("failed to start ngrok tunnel", "error", err)
			os.Exit(1)
		}
		ngrokTunnel = t
		slog.Info("ngrok tunnel available", "url", t.URL())
	}

	if ngrokTunnel != nil {
		defer ngrokTunnel.Stop()
	}

	if *openBrowser {
		host := *addr
		if strings.HasPrefix(host, ":") {
			host = "localhost" + host
		}
		targetURL := fmt.Sprintf("http://%s/", host)
		go func() {
			time.Sleep(500 * time.Millisecond)
			if err := browser.OpenURL(targetURL); err != nil {
				slog.Warn("failed to open browser", "url", targetURL, "error", err)
			}
		}()
	}

	if err := webserver.StartWithConfig(e, *addr, cfg); err != nil && err != http.ErrServerClosed {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}
