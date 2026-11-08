// Command fakeoidc is a stand-in OpenID Connect provider for trying single sign-on on a laptop (design
// note 031). It is not a real identity provider: it shows a form and signs in whoever you type. Never point a
// real Forgeline at it.
//
//	go run ./scripts/dev/fakeoidc -listen 127.0.0.1:9111 -client forgeline -secret dev-secret
//
// Then, in Forgeline, Settings › Sign-in (SSO): issuer http://127.0.0.1:9111, client ID forgeline, client
// secret dev-secret, and Forgeline's own address (for example http://127.0.0.1:8080).
package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/forgelinehq/forgeline/internal/oidcfake"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:9111", "address to listen on (keep it on loopback)")
	client := flag.String("client", "forgeline", "client ID Forgeline is configured with")
	secret := flag.String("secret", "dev-secret", "client secret Forgeline is configured with")
	flag.Parse()
	p, err := oidcfake.New(*client, *secret)
	if err != nil {
		log.Fatal(err)
	}
	p.Issuer = "http://" + *listen
	log.Printf("stand-in OIDC provider at %s (client %q); not a real provider", p.Issuer, *client)
	log.Fatal(http.ListenAndServe(*listen, p.Handler()))
}
