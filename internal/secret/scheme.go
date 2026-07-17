package secret

// scheme describes how a provider's raw key is turned into an HTTP header: the
// header name to set, and an optional value prefix (e.g. "Bearer ") placed
// before the raw key.
type scheme struct {
	header string
	prefix string
}

// Header names and the bearer prefix, named once so the table below and any
// future consumer read consistently.
const (
	headerAuthorization = "Authorization"
	headerAnthropic     = "x-api-key"
	headerGoogle        = "x-goog-api-key"
	bearerPrefix        = "Bearer "
)

// defaultScheme is applied to any service not named in providerSchemes. The
// bearer scheme is the industry-common default (OpenAI, GitHub, USAi, and most
// OAuth/token APIs expect "Authorization: Bearer <key>"), so an unknown or
// newly-added provider gets a sensible, well-documented behavior rather than an
// error. Providers that need a different header MUST be added to the table.
var defaultScheme = scheme{header: headerAuthorization, prefix: bearerPrefix}

// providerSchemes maps a normalized (lower-cased) service name to its header
// scheme. It is intentionally a small, human-reviewable data table: to support
// a new non-bearer provider, add one line here. Aliases (e.g. gemini→google)
// are listed explicitly.
//
// Services deliberately absent (openai, github, usai, ...) fall through to
// defaultScheme (Authorization: Bearer).
var providerSchemes = map[string]scheme{
	"anthropic": {header: headerAnthropic, prefix: ""},
	"google":    {header: headerGoogle, prefix: ""},
	"gemini":    {header: headerGoogle, prefix: ""}, // alias for google
}

// schemeFor returns the header scheme for a normalized service name, falling
// back to defaultScheme (Authorization: Bearer) for any unlisted service.
func schemeFor(service string) scheme {
	if s, ok := providerSchemes[service]; ok {
		return s
	}
	return defaultScheme
}

// inject builds the Injection for a raw key under this scheme.
func (s scheme) inject(rawKey string) Injection {
	return Injection{Header: s.header, Value: s.prefix + rawKey}
}
