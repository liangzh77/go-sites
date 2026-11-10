package main

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var plainEnvValuePattern = regexp.MustCompile(`^[A-Za-z0-9_./:@+%-]+$`)

func buildExportText(entries []ExportKey) string {
	if len(entries) == 0 {
		return ""
	}
	counts := make(map[string]int)
	for _, entry := range entries {
		counts[exportBasePrefix(entry)]++
	}
	used := make(map[string]bool)
	var output strings.Builder
	output.WriteString("# go-sites API key export\n")
	output.WriteString("# WARNING: this text contains real API keys. Store it securely and never commit it.\n")
	output.WriteString(fmt.Sprintf("# Keys: %d\n", len(entries)))
	for _, entry := range entries {
		base := exportBasePrefix(entry)
		prefix := base
		if counts[base] > 1 {
			suffix := envIdentifier(entry.KeyAlias)
			if suffix == "" {
				suffix = envIdentifier(entry.ProviderName)
			}
			if suffix == "" {
				suffix = strings.ToUpper(shortID(entry.KeyID))
			}
			prefix = base + "_" + suffix
		}
		unique := prefix
		for index := 2; used[unique]; index++ {
			unique = fmt.Sprintf("%s_%d", prefix, index)
		}
		used[unique] = true

		output.WriteString("\n# ")
		output.WriteString(entry.ProviderName)
		output.WriteString(" / ")
		output.WriteString(entry.KeyAlias)
		if !entry.ProviderEnabled || !entry.KeyEnabled {
			output.WriteString(" [停用记录]")
		}
		output.WriteString("\n")
		writeEnvLine(&output, unique+"_PROVIDER_NAME", entry.ProviderName)
		writeEnvLine(&output, unique+"_BASE_URL", entry.BaseURL)
		writeEnvLine(&output, unique+"_PROTOCOL", entry.Protocol)
		writeEnvLine(&output, unique+"_AUTH_MODE", entry.AuthMode)
		if entry.Model != "" {
			writeEnvLine(&output, unique+"_MODEL", entry.Model)
		} else {
			output.WriteString("# " + unique + "_MODEL=<未设置>\n")
		}
		writeEnvLine(&output, unique+"_API_KEY", entry.Secret)
	}
	return output.String()
}

func exportBasePrefix(entry ExportKey) string {
	base := envIdentifier(entry.PresetID)
	if base == "" {
		base = envIdentifier(entry.ProviderName)
	}
	if base == "" {
		base = "PROVIDER_" + strings.ToUpper(shortID(entry.ProviderID))
	}
	return base
}

func envIdentifier(value string) string {
	var output strings.Builder
	underscore := false
	for _, char := range strings.ToUpper(strings.TrimSpace(value)) {
		switch {
		case char >= 'A' && char <= 'Z', char >= '0' && char <= '9':
			output.WriteRune(char)
			underscore = false
		case output.Len() > 0:
			if !underscore {
				output.WriteByte('_')
				underscore = true
			}
		}
	}
	return strings.Trim(output.String(), "_")
}

func shortID(id string) string {
	if index := strings.LastIndex(id, "_"); index >= 0 && len(id)-index-1 >= 6 {
		return id[index+1 : index+7]
	}
	if len(id) > 6 {
		return id[:6]
	}
	return id
}

func writeEnvLine(output *strings.Builder, key, value string) {
	output.WriteString(key)
	output.WriteByte('=')
	if plainEnvValuePattern.MatchString(value) {
		output.WriteString(value)
	} else {
		output.WriteString(strconv.Quote(value))
	}
	output.WriteByte('\n')
}

func sortExportKeys(entries []ExportKey) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].ProviderName == entries[j].ProviderName {
			if entries[i].KeyAlias == entries[j].KeyAlias {
				return entries[i].KeyID < entries[j].KeyID
			}
			return entries[i].KeyAlias < entries[j].KeyAlias
		}
		return entries[i].ProviderName < entries[j].ProviderName
	})
}
