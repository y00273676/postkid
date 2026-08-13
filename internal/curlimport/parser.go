// Package curlimport parses the textual command emitted by browsers' "Copy as
// cURL" action. It intentionally implements a small, non-executing shell
// lexer instead of invoking a shell: imported commands are untrusted input.
package curlimport

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"unicode"

	"go.planetmeican.com/yangguang/postkid/internal/model"
)

// ParseError describes a rejected cURL command. Position is a zero-based byte
// offset into the original command. Token is the nearest token when one is
// available, and Reason is intended to be useful directly in a UI status
// message.
type ParseError struct {
	Position int
	Token    string
	Reason   string
}

func (e *ParseError) Error() string {
	if e == nil {
		return "curl import error"
	}
	where := fmt.Sprintf("position %d", e.Position)
	if e.Token != "" {
		where += fmt.Sprintf(" near %q", e.Token)
	}
	return "curl import error at " + where + ": " + e.Reason
}

// Error is kept as a short alias for callers that prefer a generic name while
// retaining ParseError as the discoverable concrete type for errors.As.
type Error = ParseError

// Parse parses one curl command into a model.Request. The request Name is left
// empty because a command contains no reliable collection/request name; the
// caller should assign one before saving it to a collection.
func Parse(command string) (model.Request, error) {
	tokens, err := lex(command)
	if err != nil {
		return model.Request{}, err
	}
	if len(tokens) == 0 {
		return model.Request{}, &ParseError{Reason: "command is empty"}
	}
	if !isCurlCommand(tokens[0].value) {
		err := parseError(tokens[0], "expected a curl command")
		if err.Position == tokens[0].start {
			err.Position++
		}
		return model.Request{}, err
	}

	p := commandParser{tokens: tokens, headers: make(map[string]string)}
	if err := p.parse(); err != nil {
		return model.Request{}, err
	}
	return p.request()
}

// ParseCurl is an explicit alias for Parse, useful at call sites where more
// than one command language may be accepted in the future.
func ParseCurl(command string) (model.Request, error) { return Parse(command) }

type shellState uint8

const (
	stateUnquoted shellState = iota
	stateSingle
	stateDouble
)

type token struct {
	value  string
	start  int
	quoted bool
}

// lex handles only the quoting and escaping needed by browser-generated cURL
// commands. It never expands variables, globs, redirects, pipes, or executes
// substitutions. Unsupported shell syntax is rejected rather than treated as
// an opaque argument with surprising semantics.
func lex(input string) ([]token, error) {
	var tokens []token
	var value strings.Builder
	state := stateUnquoted
	tokenStart := -1
	haveToken := false
	quotedToken := false

	flush := func() {
		if !haveToken {
			return
		}
		tokens = append(tokens, token{value: value.String(), start: tokenStart, quoted: quotedToken})
		value.Reset()
		tokenStart = -1
		haveToken = false
		quotedToken = false
	}
	begin := func(pos int) {
		if !haveToken {
			tokenStart = pos
			haveToken = true
		}
	}

	for i := 0; i < len(input); {
		ch := input[i]
		if ch == 0 {
			return nil, &ParseError{Position: i, Reason: "NUL bytes are not allowed"}
		}

		// Command substitution markers are never part of the supported import
		// grammar, including inside quotes. Rejecting them keeps a copied shell
		// fragment from being mistaken for request data.
		if ch == '`' {
			return nil, &ParseError{Position: i, Reason: "backtick command substitution is not supported"}
		}
		if ch == '$' && i+1 < len(input) && input[i+1] == '(' {
			return nil, &ParseError{Position: i, Reason: "command substitution is not supported"}
		}

		switch state {
		case stateSingle:
			if ch == '\'' {
				state = stateUnquoted
				i++
				continue
			}
			begin(i)
			value.WriteByte(ch)
			i++
		case stateDouble:
			switch ch {
			case '"':
				state = stateUnquoted
				i++
			case '\\':
				next, nextPos, ok := escapedByte(input, i)
				if !ok {
					return nil, &ParseError{Position: i, Reason: "dangling backslash"}
				}
				if next == '\n' {
					i = nextPos
					continue
				}
				begin(i)
				// POSIX double quotes only consume a backslash before $, `,
				// ", \\, or a newline. Otherwise it remains literal.
				if next == '$' || next == '`' || next == '"' || next == '\\' {
					value.WriteByte(next)
					i = nextPos
				} else {
					value.WriteByte('\\')
					i++
				}
			default:
				if ch == '$' && isExpansionStart(input, i) {
					return nil, &ParseError{Position: i, Reason: "shell variable expansion is not supported"}
				}
				if ch == '\n' {
					return nil, &ParseError{Position: i, Reason: "unescaped newline is not allowed"}
				}
				begin(i)
				value.WriteByte(ch)
				i++
			}
		default:
			switch {
			case isSpace(ch):
				flush()
				i++
			case ch == '\'':
				begin(i)
				quotedToken = true
				state = stateSingle
				i++
			case ch == '"':
				begin(i)
				quotedToken = true
				state = stateDouble
				i++
			case ch == '\\':
				next, nextPos, ok := escapedByte(input, i)
				if !ok {
					return nil, &ParseError{Position: i, Reason: "dangling backslash"}
				}
				if next == '\n' {
					i = nextPos
					continue
				}
				begin(i)
				value.WriteByte(next)
				i = nextPos
			case ch == '\n' || ch == '\r':
				return nil, &ParseError{Position: i, Reason: "unescaped newline is not allowed"}
			case isShellOperator(ch):
				return nil, &ParseError{Position: i, Reason: "shell operators are not supported"}
			case ch == '#':
				// A # at the beginning of a token would be a shell comment.
				// Treating it as data would silently change command meaning.
				if !haveToken {
					return nil, &ParseError{Position: i, Reason: "shell comments are not supported"}
				}
				value.WriteByte(ch)
				i++
			default:
				if ch == '$' && isExpansionStart(input, i) {
					return nil, &ParseError{Position: i, Reason: "shell variable expansion is not supported"}
				}
				begin(i)
				value.WriteByte(ch)
				i++
			}
		}
	}

	if state != stateUnquoted {
		return nil, &ParseError{Position: tokenStart, Reason: "unterminated quote"}
	}
	flush()
	return tokens, nil
}

func escapedByte(input string, slash int) (byte, int, bool) {
	if slash+1 >= len(input) {
		return 0, slash, false
	}
	next := input[slash+1]
	if next == '\r' && slash+2 < len(input) && input[slash+2] == '\n' {
		return '\n', slash + 3, true
	}
	return next, slash + 2, true
}

func isSpace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\v' || ch == '\f'
}

func isShellOperator(ch byte) bool {
	switch ch {
	case ';', '|', '&', '<', '>':
		return true
	default:
		return false
	}
}

func isExpansionStart(input string, pos int) bool {
	if pos+1 >= len(input) {
		return false
	}
	next := input[pos+1]
	return next == '(' || next == '{' || next == '*' || next == '?' || next == '#' ||
		next == '@' || next == '!' || next == '$' || next == '-' ||
		unicode.IsLetter(rune(next)) || next == '_'
}

func isCurlCommand(value string) bool {
	base := filepath.Base(strings.ReplaceAll(value, "\\", "/"))
	return base == "curl" || base == "curl.exe"
}

type commandParser struct {
	tokens []token
	index  int

	url           string
	urlToken      token
	hasURL        bool
	explicit      string
	hasMethod     bool
	get           bool
	lastArgQuoted bool

	data      []dataArgument
	hasData   bool
	headers   map[string]string
	headerKey map[string]string

	authType string
	username string
	password string
}

type dataArgument struct {
	value      string
	urlEncoded bool
}

func (p *commandParser) parse() error {
	p.index = 1
	p.headerKey = make(map[string]string)
	endOptions := false
	for p.index < len(p.tokens) {
		current := p.tokens[p.index]
		p.index++
		value := current.value
		if !endOptions && value == "--" {
			endOptions = true
			continue
		}
		if !endOptions && strings.HasPrefix(value, "--") {
			if err := p.longOption(current); err != nil {
				return err
			}
			continue
		}
		if !endOptions && strings.HasPrefix(value, "-") && value != "-" {
			if err := p.shortOption(current); err != nil {
				return err
			}
			continue
		}
		if p.hasURL {
			return parseError(current, "more than one URL was provided")
		}
		p.url, p.urlToken, p.hasURL = value, current, true
	}
	if !p.hasURL {
		return &ParseError{Position: lenInputPosition(p.tokens), Reason: "a URL is required"}
	}
	return nil
}

func (p *commandParser) longOption(current token) error {
	name, attached, hasAttached := splitLongOption(current.value)
	switch name {
	case "--request":
		arg, err := p.argument(current, attached, hasAttached, "request method")
		if err != nil {
			return err
		}
		return p.setMethod(current, arg)
	case "--header":
		arg, err := p.argument(current, attached, hasAttached, "header")
		if err != nil {
			return err
		}
		return p.addHeader(current, arg)
	case "--data", "--data-raw":
		arg, err := p.argument(current, attached, hasAttached, "request data")
		if err != nil {
			return err
		}
		return p.addData(current, arg, false)
	case "--data-binary":
		arg, err := p.argument(current, attached, hasAttached, "request data")
		if err != nil {
			return err
		}
		// Binary input is accepted for quoted textual payloads. An unquoted
		// value is commonly a shell/path fragment and is outside the safe
		// import subset; in particular this prevents accidental file-style
		// invocations from being treated as body text.
		if !p.lastArgQuoted && !hasAttached {
			return parseError(current, "--data-binary requires a quoted request body")
		}
		return p.addData(current, arg, false)
	case "--data-urlencode":
		arg, err := p.argument(current, attached, hasAttached, "URL-encoded request data")
		if err != nil {
			return err
		}
		return p.addData(current, arg, true)
	case "--user":
		arg, err := p.argument(current, attached, hasAttached, "user credentials")
		if err != nil {
			return err
		}
		return p.setUser(current, arg)
	case "--url":
		arg, err := p.argument(current, attached, hasAttached, "URL")
		if err != nil {
			return err
		}
		if p.hasURL {
			return parseError(current, "more than one URL was provided")
		}
		p.url, p.urlToken, p.hasURL = arg, current, true
	case "--get":
		if hasAttached {
			return parseError(current, "--get does not take a value")
		}
		p.get = true
	case "--location", "--compressed", "--silent", "--show-error", "--verbose", "--include", "--fail", "--fail-with-body", "--no-progress-meter":
		if hasAttached {
			return parseError(current, "option does not take a value")
		}
		// These options affect curl's transport/output behavior, not the
		// request definition represented by model.Request. They are safe to
		// ignore while importing.
	default:
		return parseError(current, "unsupported or unsafe curl option")
	}
	return nil
}

func (p *commandParser) shortOption(current token) error {
	value := current.value
	if len(value) > 2 && value[0] == '-' {
		// Only output/diagnostic flags may be clustered. Value-taking options
		// are handled below so -XPOST and -HHeader retain their argument.
		if value[1] != 'X' && value[1] != 'H' && value[1] != 'd' && value[1] != 'u' {
			for i := 1; i < len(value); i++ {
				if !isSafeShortFlag(value[i]) {
					return parseError(current, "unsupported or unsafe curl option")
				}
			}
			return nil
		}
	}
	// Options with values support both -X POST and compact -XPOST forms.
	for len(value) > 1 {
		name := value[:2]
		switch name {
		case "-X", "-H", "-d", "-u":
			attached := strings.TrimPrefix(value[2:], "=")
			hasAttached := value[2:] != ""
			arg, err := p.argument(current, attached, hasAttached, shortOptionName(name))
			if err != nil {
				return err
			}
			switch name {
			case "-X":
				return p.setMethod(current, arg)
			case "-H":
				return p.addHeader(current, arg)
			case "-d":
				return p.addData(current, arg, false)
			case "-u":
				return p.setUser(current, arg)
			}
		}
		// A compact argument option consumed the remainder. If the option
		// had no attached value, argument() consumed the next token.
		if name == "-X" || name == "-H" || name == "-d" || name == "-u" {
			return nil
		}
		if value == "-G" {
			p.get = true
			return nil
		}
		// Safe output-only flags may be clustered as in curl -sS.
		if !isSafeShortFlag(value[1]) {
			return parseError(current, "unsupported or unsafe curl option")
		}
		value = "-" + value[2:]
	}
	if value == "-G" {
		p.get = true
		return nil
	}
	return parseError(current, "unsupported or unsafe curl option")
}

func shortOptionName(name string) string {
	switch name {
	case "-X":
		return "request method"
	case "-H":
		return "header"
	case "-d":
		return "request data"
	case "-u":
		return "user credentials"
	default:
		return "option argument"
	}
}

func isSafeShortFlag(ch byte) bool {
	switch ch {
	case 's', 'S', 'v', 'i':
		return true
	default:
		return false
	}
}

func splitLongOption(value string) (name, attached string, hasAttached bool) {
	if index := strings.IndexByte(value, '='); index >= 0 {
		return value[:index], value[index+1:], true
	}
	return value, "", false
}

func (p *commandParser) argument(option token, attached string, hasAttached bool, description string) (string, error) {
	if hasAttached {
		return attached, nil
	}
	if p.index >= len(p.tokens) {
		return "", parseError(option, "missing "+description+" argument")
	}
	arg := p.tokens[p.index]
	p.index++
	p.lastArgQuoted = arg.quoted
	return arg.value, nil
}

func (p *commandParser) setMethod(option token, method string) error {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		return parseError(option, "request method cannot be empty")
	}
	if !model.IsValidMethod(method) {
		return parseError(option, fmt.Sprintf("unsupported HTTP method %q", method))
	}
	p.explicit, p.hasMethod = method, true
	return nil
}

func (p *commandParser) addHeader(option token, raw string) error {
	colon := strings.IndexByte(raw, ':')
	if colon <= 0 {
		return parseError(option, "header must have the form Name: value")
	}
	name := strings.TrimSpace(raw[:colon])
	value := strings.TrimSpace(raw[colon+1:])
	if name == "" {
		return parseError(option, "header name cannot be empty")
	}
	if strings.IndexFunc(name, func(r rune) bool { return r <= 0x20 || r == 0x7f || r == ':' }) >= 0 {
		return parseError(option, "header name contains invalid characters")
	}
	if strings.ContainsAny(value, "\r\n") {
		return parseError(option, "header value cannot contain a newline")
	}
	canonical := strings.ToLower(name)
	if previous, ok := p.headerKey[canonical]; ok {
		// model.Request uses a map and cannot represent duplicate wire
		// headers. Keep the first spelling while applying curl's last-value
		// behavior deterministically.
		p.headers[previous] = value
		return nil
	}
	p.headerKey[canonical] = name
	p.headers[name] = value
	return nil
}

func (p *commandParser) addData(option token, raw string, urlencode bool) error {
	if strings.HasPrefix(raw, "@") {
		return parseError(option, "@file request data is not supported; files are never read during import")
	}
	if urlencode {
		if !strings.Contains(raw, "=") && strings.Contains(raw, "@") {
			return parseError(option, "@file request data is not supported; files are never read during import")
		}
		raw = encodeDataURL(raw)
	}
	if strings.ContainsAny(raw, "\r\n") {
		// Newlines can legitimately be part of a single-quoted JSON payload;
		// the lexer has already rejected unquoted command-line newlines.
	}
	p.data = append(p.data, dataArgument{value: raw, urlEncoded: urlencode})
	p.hasData = true
	return nil
}

func encodeDataURL(raw string) string {
	if index := strings.IndexByte(raw, '='); index >= 0 {
		// curl's name=content form keeps the field name and encodes the
		// content. This is the useful representation for form bodies.
		return raw[:index+1] + url.QueryEscape(raw[index+1:])
	}
	return url.QueryEscape(raw)
}

func (p *commandParser) setUser(option token, raw string) error {
	if strings.HasPrefix(raw, "@") {
		return parseError(option, "credential files are not supported")
	}
	colon := strings.IndexByte(raw, ':')
	if colon < 0 {
		return parseError(option, "credentials must have the form user:password")
	}
	user, password := raw[:colon], raw[colon+1:]
	if strings.ContainsAny(user+password, "\r\n") {
		return parseError(option, "credentials cannot contain a newline")
	}
	p.authType = model.AuthBasic
	p.username, p.password = user, password
	return nil
}

func (p *commandParser) request() (model.Request, error) {
	if !p.hasURL {
		return model.Request{}, &ParseError{Reason: "a URL is required"}
	}
	if err := validateURL(p.url); err != nil {
		return model.Request{}, parseError(p.urlToken, err.Error())
	}

	method := "GET"
	if p.hasData && !p.get {
		method = "POST"
	}
	dataParts := make([]string, 0, len(p.data))
	for _, part := range p.data {
		value := part.value
		if p.get && !part.urlEncoded {
			value = encodeGetData(value)
		}
		dataParts = append(dataParts, value)
	}
	if p.get {
		p.url = appendQuery(p.url, strings.Join(dataParts, "&"))
	}
	if p.hasMethod {
		method = p.explicit
	}

	r := model.Request{
		Method:  method,
		URL:     p.url,
		Headers: p.headers,
	}
	if p.authType != "" {
		r.AuthType = p.authType
		r.AuthUsername = p.username
		r.AuthPassword = p.password
	}
	if p.hasData && !p.get {
		bodyParts := make([]string, 0, len(p.data))
		for _, part := range p.data {
			bodyParts = append(bodyParts, part.value)
		}
		r.Body = strings.Join(bodyParts, "&")
	}
	if len(r.Headers) == 0 {
		r.Headers = nil
	}
	return r, nil
}

func validateURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("URL cannot be empty")
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("URL must be an absolute http or https URL")
	}
	return nil
}

func appendQuery(rawURL, query string) string {
	if query == "" {
		return rawURL
	}
	separator := "?"
	if strings.Contains(rawURL, "?") {
		separator = "&"
	}
	return rawURL + separator + query
}

func encodeGetData(raw string) string {
	if index := strings.IndexByte(raw, '='); index >= 0 {
		return raw[:index+1] + url.QueryEscape(raw[index+1:])
	}
	return url.QueryEscape(raw)
}

func parseError(t token, reason string) *ParseError {
	return &ParseError{Position: t.start, Token: t.value, Reason: reason}
}

func lenInputPosition(tokens []token) int {
	if len(tokens) == 0 {
		return 0
	}
	last := tokens[len(tokens)-1]
	return last.start + len(last.value)
}
