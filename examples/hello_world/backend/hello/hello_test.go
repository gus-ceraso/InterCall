package hello

import "testing"

func TestGreeting(t *testing.T) {
	tests := []struct {
		locale string
		want   string
	}{
		{locale: "en-US", want: "Hello, World!"},
		{locale: "es-MX", want: "¡Hola, World!"},
		{locale: "fr-FR", want: "Bonjour, World !"},
		{locale: "de-DE", want: "Hallo, World!"},
		{locale: "pt-BR", want: "Olá, World!"},
		{locale: "pt_BR", want: "Olá, World!"},
		{locale: "ja-JP", want: "Hello, World!"},
	}

	for _, test := range tests {
		t.Run(test.locale, func(t *testing.T) {
			if got := greeting(test.locale, "World"); got != test.want {
				t.Fatalf("greeting(%q, %q) = %q, want %q", test.locale, "World", got, test.want)
			}
		})
	}
}
