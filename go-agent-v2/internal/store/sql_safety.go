package store

import (
	"regexp"
	"strings"
)

var (
	reLiteral            = regexp.MustCompile(`'[^']*'`)
	reFirstKeyword       = regexp.MustCompile(`(?i)^\s*(\w+)`)
	reWriteKeywords      = regexp.MustCompile(`(?i)\b(INSERT|UPDATE|DELETE|MERGE|UPSERT|CREATE|ALTER|DROP|TRUNCATE|GRANT|REVOKE)\b`)
	reDangerousExec      = regexp.MustCompile(`(?i)\b(DROP|TRUNCATE|ALTER|GRANT|REVOKE|CREATE\s+DATABASE|CREATE\s+SCHEMA)\b`)
	reDangerousFunctions = regexp.MustCompile(`(?i)\b(pg_read_file|pg_read_binary_file|pg_ls_dir|pg_stat_file|` +
		`lo_import|lo_export|lo_get|lo_put|` +
		`pg_execute_server_program|dblink|dblink_exec)\b`)
	executeWhitelist = map[string]bool{"INSERT": true, "UPDATE": true, "DELETE": true, "MERGE": true}
	reSemicolon      = regexp.MustCompile(`;\s*$`)
)

func validateSingleStatement(sql string) error {
	if strings.Contains(reSemicolon.ReplaceAllString(strings.TrimSpace(sql), ""), ";") {
		return ErrMultiStatement
	}
	return nil
}

func ValidateReadOnlyQuery(sql string) error {
	if err := validateSingleStatement(sql); err != nil {
		return err
	}
	stripped := reLiteral.ReplaceAllString(sql, "''")
	if reWriteKeywords.MatchString(stripped) {
		return ErrReadOnlyViolation
	}
	if reDangerousFunctions.MatchString(stripped) {
		return ErrDangerousSQL
	}
	return nil
}

func ValidateExecuteQuery(sql string) error {
	if err := validateSingleStatement(sql); err != nil {
		return err
	}
	keyword := ""
	if m := reFirstKeyword.FindStringSubmatch(sql); len(m) >= 2 {
		keyword = strings.ToUpper(m[1])
	}
	if !executeWhitelist[keyword] || reDangerousExec.MatchString(reLiteral.ReplaceAllString(sql, "''")) {
		return ErrDangerousSQL
	}
	return nil
}
