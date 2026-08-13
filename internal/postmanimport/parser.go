// Package postmanimport parses Postman Collection v2.1 JSON exports.
//
// The importer deliberately converts a collection into postkid's small,
// flat model. Postman folders are retained in request names (using the
// "Folder › Request" convention), while fields postkid cannot represent are
// rejected instead of being silently discarded.
package postmanimport

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"go.planetmeican.com/yangguang/postkid/internal/model"
)

const maxImportDepth = 64

// Result is the result of importing one Postman collection.
type Result struct {
	Collection model.Collection
	Imported   int
}

// Parse imports one Postman Collection v2.1 JSON document.
func Parse(data []byte) (Result, error) {
	root, err := decodeRoot(data)
	if err != nil {
		return Result{}, err
	}

	infoRaw, ok := root["info"]
	if !ok {
		return Result{}, importError("info", "field is required")
	}
	info, err := decodeObject(infoRaw, "info")
	if err != nil {
		return Result{}, err
	}
	name, err := requiredString(info, "name", "info")
	if err != nil {
		return Result{}, err
	}
	name, err = sanitizeName(name, "info.name")
	if err != nil {
		return Result{}, err
	}
	schema, err := requiredString(info, "schema", "info")
	if err != nil {
		return Result{}, err
	}
	if !isV21Schema(schema) {
		return Result{}, importError("info.schema", "expected a Postman Collection v2.1 schema, got %q", schema)
	}

	itemRaw, ok := root["item"]
	if !ok {
		return Result{}, importError("item", "field is required")
	}
	items, err := decodeArray(itemRaw, "item")
	if err != nil {
		return Result{}, err
	}

	collection := model.Collection{
		Name:      name,
		Variables: map[string]string{},
		Requests:  make([]model.Request, 0),
	}
	if variableRaw, ok := root["variable"]; ok {
		collection.Variables, err = parseVariables(variableRaw, "collection.variable")
		if err != nil {
			return Result{}, err
		}
	}

	collectionAuth := authConfig{}
	if authRaw, ok := root["auth"]; ok {
		if !isJSONNull(authRaw) {
			collectionAuth, err = parseAuth(authRaw, "collection.auth")
			if err != nil {
				return Result{}, err
			}
		}
	}

	requests := make([]model.Request, 0)
	seen := make(map[string]string)
	for i, itemRaw := range items {
		itemPath := fmt.Sprintf("item[%d]", i)
		if err := walkItem(itemRaw, nil, itemPath, collectionAuth, seen, &requests); err != nil {
			return Result{}, err
		}
	}
	collection.Requests = requests
	return Result{Collection: collection, Imported: len(requests)}, nil
}

// ParseCollection is an explicit alias for Parse for callers that prefer a
// name which identifies the accepted document type.
func ParseCollection(data []byte) (Result, error) { return Parse(data) }

type authConfig struct {
	typ      string
	username string
	password string
	token    string
}

func walkItem(raw json.RawMessage, folders []string, itemPath string, inherited authConfig, seen map[string]string, requests *[]model.Request) error {
	item, err := decodeObject(raw, itemPath)
	if err != nil {
		return err
	}
	name, err := requiredString(item, "name", itemPath)
	if err != nil {
		return err
	}
	name, err = sanitizeName(name, itemPath+".name")
	if err != nil {
		return err
	}

	auth := inherited
	if authRaw, ok := item["auth"]; ok {
		if !isJSONNull(authRaw) {
			auth, err = parseAuth(authRaw, itemPath+".auth")
			if err != nil {
				return err
			}
		}
	}

	childRaw, hasChildren := item["item"]
	requestRaw, hasRequest := item["request"]
	if hasChildren && hasRequest {
		return importError(itemPath, "cannot contain both item and request")
	}
	if hasChildren {
		if len(folders) >= maxImportDepth {
			return importError(itemPath, "folder nesting exceeds maximum depth %d", maxImportDepth)
		}
		children, err := decodeArray(childRaw, itemPath+".item")
		if err != nil {
			return err
		}
		nextFolders := append(append([]string(nil), folders...), name)
		for i, child := range children {
			childPath := fmt.Sprintf("%s.item[%d]", itemPath, i)
			if err := walkItem(child, nextFolders, childPath, auth, seen, requests); err != nil {
				return err
			}
		}
		return nil
	}
	if !hasRequest {
		return importError(itemPath, "must contain either item (folder) or request")
	}

	requestParts := append(append([]string(nil), folders...), name)
	requestPath := itemPath + ".request (" + strings.Join(requestParts, " › ") + ")"
	request, err := parseRequest(requestRaw, requestParts, requestPath, auth)
	if err != nil {
		return err
	}
	if previous, exists := seen[request.Name]; exists {
		return importError(itemPath, "duplicate request name %q; first occurrence is at %s", request.Name, previous)
	}
	seen[request.Name] = itemPath
	*requests = append(*requests, request)
	return nil
}

func parseRequest(raw json.RawMessage, pathParts []string, path string, inherited authConfig) (model.Request, error) {
	obj, err := decodeObject(raw, path)
	if err != nil {
		return model.Request{}, err
	}
	method, err := requiredString(obj, "method", path)
	if err != nil {
		return model.Request{}, err
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	if !model.IsValidMethod(method) {
		return model.Request{}, importError(path+".method", "unsupported HTTP method %q", method)
	}

	requestName := strings.Join(pathParts, " › ")
	request := model.Request{Name: requestName, Method: method, Headers: map[string]string{}, Params: map[string]string{}}

	urlRaw, ok := obj["url"]
	if !ok {
		return model.Request{}, importError(path+".url", "field is required")
	}
	request.URL, request.Params, err = parseURL(urlRaw, path+".url")
	if err != nil {
		return model.Request{}, err
	}

	if headerRaw, ok := obj["header"]; ok {
		request.Headers, err = parseHeaders(headerRaw, path+".header")
		if err != nil {
			return model.Request{}, err
		}
	}
	if bodyRaw, ok := obj["body"]; ok {
		if !isJSONNull(bodyRaw) {
			var contentType string
			request.Body, contentType, err = parseBody(bodyRaw, path+".body")
			if err == nil && contentType != "" {
				if strings.HasPrefix(contentType, "multipart/form-data;") {
					if headerKey, existing, ok := findHeader(request.Headers, "Content-Type"); ok {
						if !strings.EqualFold(strings.TrimSpace(strings.SplitN(existing, ";", 2)[0]), "multipart/form-data") {
							return model.Request{}, importError(path+".header", "multipart body conflicts with Content-Type %q", existing)
						}
						request.Headers[headerKey] = contentType
					} else {
						request.Headers["Content-Type"] = contentType
					}
				} else if !hasHeader(request.Headers, "Content-Type") {
					request.Headers["Content-Type"] = contentType
				}
			}
		}
		if err != nil {
			return model.Request{}, err
		}
	}

	auth := inherited
	if authRaw, ok := obj["auth"]; ok {
		if !isJSONNull(authRaw) {
			auth, err = parseAuth(authRaw, path+".auth")
			if err != nil {
				return model.Request{}, err
			}
		}
	}
	request.AuthType = auth.typ
	request.AuthUsername = auth.username
	request.AuthPassword = auth.password
	request.AuthToken = auth.token
	return request, nil
}

func parseURL(raw json.RawMessage, path string) (string, map[string]string, error) {
	params := make(map[string]string)
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", nil, importError(path, "must be a URL string or object")
	}
	var stringURL string
	if err := json.Unmarshal(raw, &stringURL); err == nil {
		if strings.TrimSpace(stringURL) == "" {
			return "", nil, importError(path, "URL must not be empty")
		}
		return stringURL, params, validateURL(stringURL, path)
	}

	obj, err := decodeObject(raw, path)
	if err != nil {
		return "", nil, err
	}
	urlString := ""
	if rawField, ok := obj["raw"]; ok {
		urlString, err = decodeString(rawField, path+".raw")
		if err != nil {
			return "", nil, err
		}
	}
	if strings.TrimSpace(urlString) == "" {
		urlString, err = composeURL(obj, path)
		if err != nil {
			return "", nil, err
		}
	}
	if queryRaw, ok := obj["query"]; ok {
		params, err = parseQuery(queryRaw, path+".query")
		if err != nil {
			return "", nil, err
		}
		urlString = stripURLQuery(urlString)
	}
	if strings.TrimSpace(urlString) == "" {
		return "", nil, importError(path, "URL must not be empty")
	}
	return urlString, params, validateURL(urlString, path)
}

func stripURLQuery(raw string) string {
	question := strings.IndexByte(raw, '?')
	if question < 0 {
		return raw
	}
	fragment := strings.IndexByte(raw[question+1:], '#')
	if fragment < 0 {
		return raw[:question]
	}
	return raw[:question] + raw[question+1+fragment:]
}

func composeURL(obj map[string]json.RawMessage, path string) (string, error) {
	protocol := ""
	if raw, ok := obj["protocol"]; ok {
		var err error
		protocol, err = decodeString(raw, path+".protocol")
		if err != nil {
			return "", err
		}
	}
	host := ""
	if raw, ok := obj["host"]; ok {
		var err error
		host, err = decodeStringArrayOrString(raw, path+".host", ".")
		if err != nil {
			return "", err
		}
	}
	pathPart := ""
	if raw, ok := obj["path"]; ok {
		var err error
		pathPart, err = decodeStringArrayOrString(raw, path+".path", "/")
		if err != nil {
			return "", err
		}
	}
	if protocol != "" && host != "" {
		return protocol + "://" + host + strings.TrimPrefix("/"+pathPart, "//"), nil
	}
	if host != "" {
		return host + strings.TrimPrefix("/"+pathPart, "//"), nil
	}
	if pathPart != "" {
		return pathPart, nil
	}
	return "", importError(path, "URL object has no raw or usable protocol/host/path")
}

func parseQuery(raw json.RawMessage, path string) (map[string]string, error) {
	items, err := decodeArray(raw, path)
	if err != nil {
		return nil, err
	}
	params := make(map[string]string)
	for i, itemRaw := range items {
		itemPath := fmt.Sprintf("%s[%d]", path, i)
		obj, err := decodeObject(itemRaw, itemPath)
		if err != nil {
			return nil, err
		}
		disabled, err := optionalBool(obj, "disabled", itemPath)
		if err != nil {
			return nil, err
		}
		if disabled {
			continue
		}
		key, err := requiredString(obj, "key", itemPath)
		if err != nil {
			return nil, err
		}
		value := ""
		if valueRaw, ok := obj["value"]; ok {
			value, err = scalarString(valueRaw, itemPath+".value")
			if err != nil {
				return nil, err
			}
		}
		if _, exists := params[key]; exists {
			return nil, importError(itemPath, "duplicate enabled query key %q", key)
		}
		params[key] = value
	}
	return params, nil
}

func parseHeaders(raw json.RawMessage, path string) (map[string]string, error) {
	items, err := decodeArray(raw, path)
	if err != nil {
		return nil, err
	}
	headers := make(map[string]string)
	for i, itemRaw := range items {
		itemPath := fmt.Sprintf("%s[%d]", path, i)
		obj, err := decodeObject(itemRaw, itemPath)
		if err != nil {
			return nil, err
		}
		disabled, err := optionalBool(obj, "disabled", itemPath)
		if err != nil {
			return nil, err
		}
		if disabled {
			continue
		}
		key, err := requiredString(obj, "key", itemPath)
		if err != nil {
			return nil, err
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, importError(itemPath+".key", "must not be empty")
		}
		if !validHTTPHeaderName(key) {
			return nil, importError(itemPath+".key", "invalid HTTP header name %q", key)
		}
		for existing := range headers {
			if strings.EqualFold(existing, key) {
				return nil, importError(itemPath+".key", "duplicate header %q (case-insensitive)", key)
			}
		}
		value := ""
		if valueRaw, ok := obj["value"]; ok {
			value, err = scalarString(valueRaw, itemPath+".value")
			if err != nil {
				return nil, err
			}
		}
		if strings.ContainsAny(value, "\r\n") {
			return nil, importError(itemPath+".value", "header value must not contain a newline")
		}
		headers[key] = value
	}
	return headers, nil
}

func hasHeader(headers map[string]string, key string) bool {
	_, _, ok := findHeader(headers, key)
	return ok
}

func findHeader(headers map[string]string, key string) (string, string, bool) {
	for existing := range headers {
		if strings.EqualFold(existing, key) {
			return existing, headers[existing], true
		}
	}
	return "", "", false
}

func validHTTPHeaderName(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		if !strings.ContainsRune("!#$%&'*+-.^_`|~", r) {
			return false
		}
	}
	return true
}

func parseBody(raw json.RawMessage, path string) (string, string, error) {
	obj, err := decodeObject(raw, path)
	if err != nil {
		return "", "", err
	}
	mode, err := requiredString(obj, "mode", path)
	if err != nil {
		return "", "", err
	}
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "raw":
		valueRaw, ok := obj["raw"]
		if !ok {
			return "", rawContentType(obj), nil
		}
		value, err := decodeString(valueRaw, path+".raw")
		if err != nil {
			return "", "", err
		}
		return value, rawContentType(obj), nil
	case "urlencoded":
		value, err := parseKeyValueBody(obj, "urlencoded", path)
		return value, "application/x-www-form-urlencoded", err
	case "formdata":
		value, boundary, err := parseMultipartBody(obj, path)
		if err != nil {
			return "", "", err
		}
		return value, "multipart/form-data; boundary=" + boundary, nil
	default:
		return "", "", importError(path+".mode", "unsupported body mode %q (supported: raw, urlencoded, formdata)", mode)
	}
}

func rawContentType(obj map[string]json.RawMessage) string {
	optionsRaw, ok := obj["options"]
	if !ok || isJSONNull(optionsRaw) {
		return ""
	}
	options, err := decodeObject(optionsRaw, "body.options")
	if err != nil {
		return ""
	}
	rawRaw, ok := options["raw"]
	if !ok {
		return ""
	}
	rawOptions, err := decodeObject(rawRaw, "body.options.raw")
	if err != nil {
		return ""
	}
	languageRaw, ok := rawOptions["language"]
	if !ok {
		return ""
	}
	language, err := decodeString(languageRaw, "body.options.raw.language")
	if err != nil {
		return ""
	}
	if strings.EqualFold(language, "json") {
		return "application/json"
	}
	return ""
}

func parseKeyValueBody(obj map[string]json.RawMessage, field, path string) (string, error) {
	raw, ok := obj[field]
	if !ok {
		return "", importError(path+"."+field, "field is required")
	}
	items, err := decodeArray(raw, path+"."+field)
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, len(items))
	for i, itemRaw := range items {
		itemPath := fmt.Sprintf("%s.%s[%d]", path, field, i)
		obj, err := decodeObject(itemRaw, itemPath)
		if err != nil {
			return "", err
		}
		disabled, err := optionalBool(obj, "disabled", itemPath)
		if err != nil {
			return "", err
		}
		if disabled {
			continue
		}
		if typeRaw, ok := obj["type"]; ok {
			typeName, err := decodeString(typeRaw, itemPath+".type")
			if err != nil {
				return "", err
			}
			if strings.EqualFold(typeName, "file") {
				return "", importError(itemPath, "file form item is not supported")
			}
		}
		if _, ok := obj["src"]; ok {
			return "", importError(itemPath, "file form item is not supported")
		}
		if valueRaw, ok := obj["value"]; ok && isCompositeJSON(valueRaw) {
			return "", importError(itemPath, "file form item is not supported")
		}
		key, err := requiredString(obj, "key", itemPath)
		if err != nil {
			return "", err
		}
		value := ""
		if valueRaw, ok := obj["value"]; ok {
			value, err = scalarString(valueRaw, itemPath+".value")
			if err != nil {
				return "", err
			}
		}
		parts = append(parts, url.QueryEscape(key)+"="+url.QueryEscape(value))
	}
	return strings.Join(parts, "&"), nil
}

func parseMultipartBody(obj map[string]json.RawMessage, path string) (string, string, error) {
	raw, ok := obj["formdata"]
	if !ok {
		return "", "", importError(path+".formdata", "field is required")
	}
	items, err := decodeArray(raw, path+".formdata")
	if err != nil {
		return "", "", err
	}
	type part struct{ key, value string }
	parts := make([]part, 0, len(items))
	for i, itemRaw := range items {
		itemPath := fmt.Sprintf("%s.formdata[%d]", path, i)
		item, err := decodeObject(itemRaw, itemPath)
		if err != nil {
			return "", "", err
		}
		disabled, err := optionalBool(item, "disabled", itemPath)
		if err != nil {
			return "", "", err
		}
		if disabled {
			continue
		}
		if typeRaw, ok := item["type"]; ok {
			typeName, err := decodeString(typeRaw, itemPath+".type")
			if err != nil {
				return "", "", err
			}
			if strings.EqualFold(typeName, "file") {
				return "", "", importError(itemPath, "file form item is not supported")
			}
		}
		if _, ok := item["src"]; ok {
			return "", "", importError(itemPath, "file form item is not supported")
		}
		key, err := requiredString(item, "key", itemPath)
		if err != nil {
			return "", "", err
		}
		if strings.ContainsAny(key, "\r\n") {
			return "", "", importError(itemPath+".key", "must not contain a newline")
		}
		value := ""
		if valueRaw, ok := item["value"]; ok {
			value, err = scalarString(valueRaw, itemPath+".value")
			if err != nil {
				return "", "", err
			}
		}
		parts = append(parts, part{key: key, value: value})
	}
	seed := strings.Builder{}
	for _, item := range parts {
		seed.WriteString(item.key)
		seed.WriteByte('=')
		seed.WriteString(item.value)
		seed.WriteByte('\x00')
	}
	hash := sha256.Sum256([]byte(seed.String()))
	boundary := "----------------postkid-" + fmt.Sprintf("%x", hash[:8])
	var body strings.Builder
	for _, item := range parts {
		body.WriteString("--")
		body.WriteString(boundary)
		body.WriteString("\r\nContent-Disposition: form-data; name=\"")
		body.WriteString(escapeMultipartName(item.key))
		body.WriteString("\"\r\n\r\n")
		body.WriteString(item.value)
		body.WriteString("\r\n")
	}
	body.WriteString("--")
	body.WriteString(boundary)
	body.WriteString("--\r\n")
	return body.String(), boundary, nil
}

func escapeMultipartName(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, `"`, `\"`)
}

func parseVariables(raw json.RawMessage, path string) (map[string]string, error) {
	items, err := decodeArray(raw, path)
	if err != nil {
		return nil, err
	}
	variables := make(map[string]string)
	for i, itemRaw := range items {
		itemPath := fmt.Sprintf("%s[%d]", path, i)
		obj, err := decodeObject(itemRaw, itemPath)
		if err != nil {
			return nil, err
		}
		disabled, err := optionalBool(obj, "disabled", itemPath)
		if err != nil {
			return nil, err
		}
		if disabled {
			continue
		}
		key, err := requiredString(obj, "key", itemPath)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(key) == "" {
			return nil, importError(itemPath+".key", "must not be empty")
		}
		if !validVariableName(key) {
			return nil, importError(itemPath+".key", "variable name %q is not supported; use letters, digits, or underscore", key)
		}
		valueRaw, ok := obj["value"]
		if !ok {
			valueRaw = obj["initialValue"]
		}
		value := ""
		if len(valueRaw) > 0 {
			value, err = scalarString(valueRaw, itemPath+".value")
			if err != nil {
				return nil, err
			}
		}
		if _, exists := variables[key]; exists {
			return nil, importError(itemPath, "duplicate collection variable %q", key)
		}
		variables[key] = value
	}
	return variables, nil
}

func parseAuth(raw json.RawMessage, path string) (authConfig, error) {
	obj, err := decodeObject(raw, path)
	if err != nil {
		return authConfig{}, err
	}
	typeName, err := requiredString(obj, "type", path)
	if err != nil {
		return authConfig{}, err
	}
	switch strings.ToLower(strings.TrimSpace(typeName)) {
	case "noauth", "none":
		return authConfig{typ: model.AuthNone}, nil
	case "basic":
		fields, err := authFields(obj, "basic", path)
		if err != nil {
			return authConfig{}, err
		}
		return authConfig{typ: model.AuthBasic, username: fields["username"], password: fields["password"]}, nil
	case "bearer":
		fields, err := authFields(obj, "bearer", path)
		if err != nil {
			return authConfig{}, err
		}
		return authConfig{typ: model.AuthBearer, token: fields["token"]}, nil
	default:
		return authConfig{}, importError(path+".type", "unsupported auth type %q (supported: noauth, basic, bearer)", typeName)
	}
}

func authFields(obj map[string]json.RawMessage, field, path string) (map[string]string, error) {
	raw, ok := obj[field]
	if !ok {
		return nil, importError(path+"."+field, "field is required")
	}
	items, err := decodeArray(raw, path+"."+field)
	if err != nil {
		return nil, err
	}
	fields := make(map[string]string)
	for i, itemRaw := range items {
		itemPath := fmt.Sprintf("%s.%s[%d]", path, field, i)
		item, err := decodeObject(itemRaw, itemPath)
		if err != nil {
			return nil, err
		}
		disabled, err := optionalBool(item, "disabled", itemPath)
		if err != nil {
			return nil, err
		}
		if disabled {
			continue
		}
		key, err := requiredString(item, "key", itemPath)
		if err != nil {
			return nil, err
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value := ""
		if valueRaw, ok := item["value"]; ok {
			value, err = scalarString(valueRaw, itemPath+".value")
			if err != nil {
				return nil, err
			}
		}
		fields[key] = value
	}
	return fields, nil
}

func validateURL(value, path string) error {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "{{") {
		return nil
	}
	u, err := url.Parse(value)
	if err != nil {
		return importError(path, "invalid URL %q: %v", value, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return importError(path, "unsupported URL scheme %q (expected http or https)", u.Scheme)
	}
	if u.Host == "" {
		return importError(path, "URL must include a host")
	}
	return nil
}

func isV21Schema(schema string) bool {
	u, err := url.Parse(strings.TrimSpace(schema))
	if err != nil || u.Scheme != "https" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if !strings.EqualFold(u.Host, "schema.getpostman.com") && !strings.EqualFold(u.Host, "schema.postman.com") {
		return false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	return len(parts) == 4 && parts[0] == "json" && parts[1] == "collection" &&
		parts[2] == "v2.1.0" && parts[3] == "collection.json"
}

func validVariableName(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

func sanitizeName(value, path string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", importError(path, "must not be empty")
	}
	unsafe := strings.ContainsAny(value, "/\\")
	for _, r := range value {
		if unicode.IsControl(r) {
			return "", importError(path, "must not contain control characters")
		}
	}
	if !unsafe {
		return value, nil
	}
	safe := strings.NewReplacer("/", "／", "\\", "＼").Replace(value)
	hash := sha256.Sum256([]byte(value))
	return safe + " [" + fmt.Sprintf("%x", hash[:4]) + "]", nil
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func decodeObject(raw json.RawMessage, path string) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, importError(path, "must be a JSON object")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, importError(path, "must be a JSON object: %v", err)
	}
	if object == nil {
		return nil, importError(path, "must be a JSON object")
	}
	return object, nil
}

func decodeArray(raw json.RawMessage, path string) ([]json.RawMessage, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, importError(path, "must be a JSON array")
	}
	var array []json.RawMessage
	if err := json.Unmarshal(raw, &array); err != nil {
		return nil, importError(path, "must be a JSON array: %v", err)
	}
	if array == nil {
		return nil, importError(path, "must be a JSON array")
	}
	return array, nil
}

func requiredString(obj map[string]json.RawMessage, key, path string) (string, error) {
	raw, ok := obj[key]
	if !ok {
		return "", importError(path+"."+key, "field is required")
	}
	return decodeString(raw, path+"."+key)
}

func decodeString(raw json.RawMessage, path string) (string, error) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", importError(path, "must be a string: %v", err)
	}
	return value, nil
}

func decodeStringArrayOrString(raw json.RawMessage, path, separator string) (string, error) {
	if value, err := decodeString(raw, path); err == nil {
		return value, nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return "", importError(path, "must be a string or string array: %v", err)
	}
	return strings.Join(values, separator), nil
}

func optionalBool(obj map[string]json.RawMessage, key, path string) (bool, error) {
	raw, ok := obj[key]
	if !ok {
		return false, nil
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, importError(path+"."+key, "must be a boolean: %v", err)
	}
	return value, nil
}

func scalarString(raw json.RawMessage, path string) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if bytes.Equal(trimmed, []byte("null")) {
		return "", nil
	}
	var stringValue string
	if err := json.Unmarshal(trimmed, &stringValue); err == nil {
		return stringValue, nil
	}
	var number json.Number
	if err := json.Unmarshal(trimmed, &number); err == nil {
		return number.String(), nil
	}
	var boolValue bool
	if err := json.Unmarshal(trimmed, &boolValue); err == nil {
		return strconv.FormatBool(boolValue), nil
	}
	return "", importError(path, "must be a scalar string, number, boolean, or null")
}

func isCompositeJSON(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return false
	}
	return trimmed[0] == '{' || trimmed[0] == '['
}

func importError(path, format string, args ...any) error {
	return fmt.Errorf("postman import at %s: %s", path, fmt.Sprintf(format, args...))
}

// decodeObject is intentionally used at the document boundary as well as on
// nested values. json.Decoder (rather than json.Unmarshal) makes the trailing
// data check explicit, while UseNumber avoids turning collection variables
// such as large IDs into imprecise float64 values.
func decodeRoot(data []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		return nil, importError("collection", "invalid JSON: %v", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, importError("collection", "trailing JSON data is not allowed")
		}
		return nil, importError("collection", "invalid trailing JSON data: %v", err)
	}
	return decodeObject(raw, "collection")
}
