package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode"
)

func parseHeaders(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, fmt.Errorf("--headers must be a valid JSON object: %w", err)
	}
	headers := make(map[string]string, len(decoded))
	for key, value := range decoded {
		str, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("--headers values must be strings")
		}
		headers[key] = str
	}
	return headers, nil
}

func parseHeadersFile(path string) (map[string]string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("--headers-file could not be read: %w", err)
	}
	headers, err := parseHeadersFileContent(string(content))
	if err != nil {
		return nil, fmt.Errorf("--headers-file %s", err)
	}
	return headers, nil
}

func parseHeadersFileContent(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if strings.HasPrefix(raw, "{") || strings.HasPrefix(raw, "[") {
		return parseJSONHeadersFile(raw)
	}
	if headers, matched, err := parseNetscapeCookies(raw); matched {
		return headers, err
	}
	if strings.Contains(raw, ":") {
		if headers, err := parseHeaderString(raw); err == nil {
			return headers, nil
		}
	}
	if looksLikeCookieHeaderValue(raw) {
		return map[string]string{"Cookie": normalizeCookieHeaderValue(raw)}, nil
	}
	return nil, fmt.Errorf("must contain JSON headers/cookies, an HTTP header string, a Netscape cookie file, or a Cookie header value")
}

func parseJSONHeadersFile(raw string) (map[string]string, error) {
	var object map[string]any
	if err := json.Unmarshal([]byte(raw), &object); err == nil {
		if headers, ok := stringMapHeaders(object); ok {
			return headers, nil
		}
		if value, ok := object["headers"]; ok {
			headers, err := parseJSONNamedValueHeaders(value)
			if err != nil {
				return nil, err
			}
			return headers, nil
		}
		if value, ok := object["cookies"]; ok {
			headers, err := parseJSONCookieObjects(value)
			if err != nil {
				return nil, err
			}
			return headers, nil
		}
		return nil, fmt.Errorf("JSON object must contain string header values or a headers/cookies array")
	}

	var array []map[string]any
	if err := json.Unmarshal([]byte(raw), &array); err != nil {
		return nil, fmt.Errorf("must contain valid JSON: %w", err)
	}
	if looksLikeCookieObjectArray(array) {
		return cookieHeadersFromObjects(array)
	}
	return namedValueHeadersFromObjects(array)
}

func stringMapHeaders(object map[string]any) (map[string]string, bool) {
	headers := make(map[string]string, len(object))
	for key, value := range object {
		str, ok := value.(string)
		if !ok {
			return nil, false
		}
		headers[key] = str
	}
	return headers, true
}

func parseJSONNamedValueHeaders(value any) (map[string]string, error) {
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("JSON headers field must be an array")
	}
	objects := make([]map[string]any, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("JSON headers array items must be objects")
		}
		objects = append(objects, object)
	}
	return namedValueHeadersFromObjects(objects)
}

func parseJSONCookieObjects(value any) (map[string]string, error) {
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("JSON cookies field must be an array")
	}
	objects := make([]map[string]any, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("JSON cookies array items must be objects")
		}
		objects = append(objects, object)
	}
	return cookieHeadersFromObjects(objects)
}

func namedValueHeadersFromObjects(objects []map[string]any) (map[string]string, error) {
	headers := map[string]string{}
	for _, object := range objects {
		name, value, ok := objectNameValue(object)
		if !ok {
			return nil, fmt.Errorf("JSON header array items must contain string name and value fields")
		}
		if !validHeaderName(name) {
			return nil, fmt.Errorf("JSON header array contains an invalid header name")
		}
		headers[name] = value
	}
	if len(headers) == 0 {
		return nil, fmt.Errorf("JSON header array contains no headers")
	}
	return headers, nil
}

func cookieHeadersFromObjects(objects []map[string]any) (map[string]string, error) {
	cookies := []string{}
	for _, object := range objects {
		name, value, ok := objectNameValue(object)
		if !ok {
			return nil, fmt.Errorf("JSON cookie array items must contain string name and value fields")
		}
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("JSON cookie array contains a cookie without a name")
		}
		cookies = append(cookies, name+"="+value)
	}
	if len(cookies) == 0 {
		return nil, fmt.Errorf("JSON cookie array contains no cookies")
	}
	return map[string]string{"Cookie": strings.Join(cookies, "; ")}, nil
}

func objectNameValue(object map[string]any) (string, string, bool) {
	name, nameOK := object["name"].(string)
	value, valueOK := object["value"].(string)
	return strings.TrimSpace(name), value, nameOK && valueOK
}

func looksLikeCookieObjectArray(objects []map[string]any) bool {
	if len(objects) == 0 {
		return false
	}
	cookieKeys := []string{"domain", "expirationDate", "hostOnly", "httpOnly", "path", "sameSite", "secure", "session", "storeId"}
	for _, object := range objects {
		for _, key := range cookieKeys {
			if _, ok := object[key]; ok {
				return true
			}
		}
	}
	return false
}

func mergeHeaders(base map[string]string, override map[string]string) map[string]string {
	if len(base) == 0 {
		return override
	}
	if len(override) == 0 {
		return base
	}
	merged := make(map[string]string, len(base)+len(override))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range override {
		merged[key] = value
	}
	return merged
}

func parseHeaderString(raw string) (map[string]string, error) {
	headers := map[string]string{}
	var lastName string
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if lastName != "" && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
			headers[lastName] = strings.TrimSpace(headers[lastName] + " " + strings.TrimSpace(line))
			continue
		}
		if isHTTPStartLine(line) {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), ":") {
			lastName = ""
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok || !validHeaderName(strings.TrimSpace(name)) {
			return nil, fmt.Errorf("contains an invalid HTTP header line")
		}
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if existing, ok := headers[name]; ok && existing != "" && value != "" {
			headers[name] = existing + ", " + value
		} else if existing, ok := headers[name]; ok && existing != "" {
			headers[name] = existing
		} else {
			headers[name] = value
		}
		lastName = name
	}
	if len(headers) == 0 {
		return nil, fmt.Errorf("contains no HTTP headers")
	}
	return headers, nil
}

func parseNetscapeCookies(raw string) (map[string]string, bool, error) {
	var cookies []string
	matched := false
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" {
			continue
		}
		if strings.Contains(line, "Netscape HTTP Cookie File") {
			matched = true
			continue
		}
		if strings.HasPrefix(line, "#HttpOnly_") {
			matched = true
			line = strings.TrimPrefix(line, "#HttpOnly_")
		} else if strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.SplitN(line, "\t", 7)
		if len(fields) != 7 {
			if matched {
				return nil, true, fmt.Errorf("contains an invalid Netscape cookie line")
			}
			return nil, false, nil
		}
		matched = true
		name := strings.TrimSpace(fields[5])
		value := strings.TrimSpace(fields[6])
		if name == "" {
			return nil, true, fmt.Errorf("contains a Netscape cookie without a name")
		}
		cookies = append(cookies, name+"="+value)
	}
	if !matched {
		return nil, false, nil
	}
	if len(cookies) == 0 {
		return nil, true, fmt.Errorf("contains no Netscape cookies")
	}
	return map[string]string{"Cookie": strings.Join(cookies, "; ")}, true, nil
}

func looksLikeCookieHeaderValue(raw string) bool {
	raw = normalizeCookieHeaderValue(raw)
	if raw == "" || strings.ContainsAny(raw, "\r\n") {
		return false
	}
	for _, part := range strings.Split(raw, ";") {
		name, _, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || strings.TrimSpace(name) == "" {
			return false
		}
	}
	return true
}

func normalizeCookieHeaderValue(raw string) string {
	parts := []string{}
	for _, part := range strings.Split(strings.TrimSpace(raw), ";") {
		part = strings.TrimSpace(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "; ")
}

func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r > unicode.MaxASCII || !(unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("!#$%&'*+-.^_`|~", r)) {
			return false
		}
	}
	return true
}

func isHTTPStartLine(line string) bool {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "HTTP/") {
		return true
	}
	methods := []string{"CONNECT ", "DELETE ", "GET ", "HEAD ", "OPTIONS ", "PATCH ", "POST ", "PUT ", "TRACE "}
	for _, method := range methods {
		if strings.HasPrefix(line, method) {
			return true
		}
	}
	return false
}
