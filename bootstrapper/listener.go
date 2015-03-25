package bootstrapper

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"sync"

	"github.com/cloudfoundry/bosh-agent/bootstrapper/auth"
	"github.com/cloudfoundry/bosh-agent/bootstrapper/package_installer"
	"github.com/cloudfoundry/bosh-agent/errors"
	"github.com/cloudfoundry/bosh-agent/logger"
)

type Listener struct {
	config    SSLConfig
	installer package_installer.PackageInstaller
	server    http.Server
	listener  net.Listener
	started   bool
	closing   bool
	wg        sync.WaitGroup
}

func NewListener(config SSLConfig, installer package_installer.PackageInstaller) *Listener {
	return &Listener{
		config:    config,
		installer: installer,
	}
}

func (l *Listener) ListenAndServe(logger logger.Logger, port int) error {
	certAuthRules := auth.CertificateVerifier{AllowedNames: l.config.PkixNames}

	serveMux := http.NewServeMux()
	serveMux.Handle("/self-update", certAuthRules.Wrap(logger, &SelfUpdateHandler{Logger: logger, packageInstaller: l.installer}))

	l.server.Handler = serveMux

	serverCert, err := tls.LoadX509KeyPair(l.config.CertFile, l.config.KeyFile)
	if err != nil {
		return err
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(([]byte)(l.config.CACertPem)) {
		return errors.Errorf("Huh? root PEM looks weird!\n%s\n", l.config.CACertPem)
	}
	config := &tls.Config{
		NextProtos:   []string{"http/1.1"},
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    certPool,
	}

	listener, err := net.ListenTCP("tcp", &net.TCPAddr{Port: port})
	if err != nil {
		return err
	}

	l.listener = tls.NewListener(listener, config)

	l.started = true
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		err := l.server.Serve(l.listener)
		if err != nil && !l.closing {
			logger.Error("Listener", "unexpected server shutdown: %s", err)
		}
	}()

	return nil
}

func (l *Listener) Close() {
	if l.started {
		l.closing = true
		l.listener.Close()
		l.started = false
	}
}

func (l *Listener) WaitForServerToExit() {
	l.wg.Wait()
}
