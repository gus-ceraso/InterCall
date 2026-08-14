// Package hello provides the hello-world backend procedure.
package hello

import (
	"context"
	"fmt"
	"strings"

	"example.com/intercall-hello/backend/gen/browserimport"
)

// Hello asks the calling browser for its locale and returns a localized greeting.
// @intercall procedure hello
// @param name The name to greet.
// @return A greeting localized for the calling browser.
func Hello(ctx context.Context, name string) (string, error) {
	locale, err := browserimport.Locale(ctx)
	if err != nil {
		return "", fmt.Errorf("get browser locale: %w", err)
	}
	return greeting(locale, name), nil
}

func greeting(locale, name string) string {
	normalized := strings.ToLower(strings.ReplaceAll(locale, "_", "-"))
	language, _, _ := strings.Cut(normalized, "-")

	switch {
	case normalized == "pt-br" || strings.HasPrefix(normalized, "pt-br-"):
		return fmt.Sprintf("Olá, %s!", name)
	case language == "es":
		return fmt.Sprintf("¡Hola, %s!", name)
	case language == "fr":
		return fmt.Sprintf("Bonjour, %s !", name)
	case language == "de":
		return fmt.Sprintf("Hallo, %s!", name)
	default:
		return fmt.Sprintf("Hello, %s!", name)
	}
}
