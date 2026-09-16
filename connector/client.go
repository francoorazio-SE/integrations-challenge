package main

import (
	"fmt"

	helpers "github.com/fatjonblp/coding_challange_integrations/connector/helpers/go"
)

// NewClient builds a helpers.Client against baseURL and authenticates it
// immediately, so a bad credential fails fast at startup (exit 3) instead of
// surfacing on the first real request buried inside a phase.
func NewClient(baseURL, tokenPath, clientID, clientSecret string) (*helpers.Client, error) {
	c := &helpers.Client{
		Base:         baseURL,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenPath:    tokenPath,
	}

	if err := c.Authenticate(); err != nil {
		return nil, fmt.Errorf("authenticate against %s: %w", baseURL, err)
	}

	return c, nil
}
