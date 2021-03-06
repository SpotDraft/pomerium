package main // import "github.com/pomerium/pomerium/cmd/pomerium"

import (
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/pomerium/envconfig"
	"github.com/pomerium/pomerium/authenticate"
	"github.com/pomerium/pomerium/internal/https"
	"github.com/pomerium/pomerium/internal/log"
	"github.com/pomerium/pomerium/internal/options"
	"github.com/pomerium/pomerium/internal/version"
	"github.com/pomerium/pomerium/proxy"
)

var (
	debugFlag   = flag.Bool("debug", false, "run server in debug mode, changes log output to STDOUT and level to info")
	versionFlag = flag.Bool("version", false, "prints the version")
)

func main() {
	mainOpts, err := optionsFromEnvConfig()
	if err != nil {
		log.Fatal().Err(err).Msg("cmd/pomerium: failed to parse authenticator settings")
	}
	flag.Parse()
	if *debugFlag || mainOpts.Debug {
		log.SetDebugMode()
	}
	if *versionFlag {
		fmt.Printf("%s", version.FullVersion())
		os.Exit(0)
	}
	log.Debug().Str("version", version.FullVersion()).Str("user-agent", version.UserAgent()).Msg("cmd/pomerium")

	var auth *authenticate.Authenticate
	var authHost string
	if mainOpts.Services == "all" || mainOpts.Services == "authenticate" {
		authOpts, err := authenticate.OptionsFromEnvConfig()
		if err != nil {
			log.Fatal().Err(err).Msg("cmd/pomerium: failed to parse authenticate settings")
		}
		emailValidator := func(p *authenticate.Authenticate) error {
			p.Validator = options.NewEmailValidator(authOpts.AllowedDomains)
			return nil
		}

		auth, err = authenticate.New(authOpts, emailValidator)
		if err != nil {
			log.Fatal().Err(err).Msg("cmd/pomerium: failed to create authenticate")
		}
		authHost = authOpts.RedirectURL.Host
	}

	var p *proxy.Proxy
	if mainOpts.Services == "all" || mainOpts.Services == "proxy" {
		proxyOpts, err := proxy.OptionsFromEnvConfig()
		if err != nil {
			log.Fatal().Err(err).Msg("cmd/pomerium: failed to parse proxy settings")
		}

		p, err = proxy.New(proxyOpts)
		if err != nil {
			log.Fatal().Err(err).Msg("cmd/pomerium: failed to create proxy")
		}
	}

	topMux := http.NewServeMux()
	if auth != nil {
		// Need to handle ping without host lookup for LB
		topMux.HandleFunc("/ping", func(rw http.ResponseWriter, _ *http.Request) {
			rw.WriteHeader(http.StatusOK)
			fmt.Fprintf(rw, "OK")
		})
		topMux.Handle(authHost+"/", auth.Handler())
	}
	if p != nil {
		topMux.Handle("/", p.Handler())
	}
	httpOpts := &https.Options{
		Addr:     mainOpts.Addr,
		Cert:     mainOpts.Cert,
		Key:      mainOpts.Key,
		CertFile: mainOpts.CertFile,
		KeyFile:  mainOpts.KeyFile,
	}
	log.Fatal().Err(https.ListenAndServeTLS(httpOpts, topMux)).Msg("cmd/pomerium: fatal")
}

// Options are the global environmental flags used to set up pomerium's services.
// If a base64 encoded certificate and key are not provided as environmental variables,
// or if a file location is not provided, the server will attempt to find a matching keypair
// in the local directory as `./cert.pem` and `./privkey.pem` respectively.
type Options struct {
	// Debug enables more verbose logging, and outputs human-readable logs to Stdout.
	// Set with POMERIUM_DEBUG
	Debug bool `envconfig:"POMERIUM_DEBUG"`
	// Services is a list enabled service mode. If none are selected, "all" is used.
	// Available options are : "all", "authenticate", "proxy".
	Services string `envconfig:"SERVICES"`
	// Addr specifies the host and port on which the server should serve
	// HTTPS requests. If empty, ":https" is used.
	Addr string `envconfig:"ADDRESS"`
	// Cert and Key specifies the base64 encoded TLS certificates to use.
	Cert string `envconfig:"CERTIFICATE"`
	Key  string `envconfig:"CERTIFICATE_KEY"`
	// CertFile and KeyFile specifies the TLS certificates to use.
	CertFile string `envconfig:"CERTIFICATE_FILE"`
	KeyFile  string `envconfig:"CERTIFICATE_KEY_FILE"`
}

var defaultOptions = &Options{
	Debug:    false,
	Services: "all",
}

// optionsFromEnvConfig builds the authentication service's configuration
// options from provided environmental variables
func optionsFromEnvConfig() (*Options, error) {
	o := defaultOptions
	if err := envconfig.Process("", o); err != nil {
		return nil, err
	}
	if !isValidService(o.Services) {
		return nil, fmt.Errorf("%s is an invalid service type", o.Services)
	}
	return o, nil
}

// isValidService checks to see if a service is a valid service mode
func isValidService(service string) bool {
	switch service {
	case
		"all",
		"proxy",
		"authenticate":
		return true
	}
	return false
}
