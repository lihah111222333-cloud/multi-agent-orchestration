// sql_safety.go — SQL 安全验证 (对应 Python agent_ops_store.py 5 个验证函数)。
//
// Python 函数对应:
//
//	_strip_sql_literals  → stripSQLLiterals
//	_validate_single_statement → validateSingleStatement
//	_first_sql_keyword → firstSQLKeyword
//	_validate_read_only_query → ValidateReadOnlyQuery
//	_validate_execute_query → ValidateExecuteQuery
package store

import (
	"regexp"
	"strings"
)

var (
	// 去除 SQL 字符串字面量 (单引号包裹), 避免 WHERE x = 'DROP TABLE' 误报。
	reLiteral = regexp.MustCompile(`'[^']*'`)

	// SQL 首关键词提取。
	reFirstKeyword = regexp.MustCompile(`(?i)^\s*(\w+)`)

	// 写入关键词 (在去除字面量后检测)。
	reWriteKeywords = regexp.MustCompile(`(?i)\b(INSERT|UPDATE|DELETE|MERGE|UPSERT|CREATE|ALTER|DROP|TRUNCATE|GRANT|REVOKE)\b`)

	// 危险执行关键词。
	reDangerousExec = regexp.MustCompile(`(?i)\b(DROP|TRUNCATE|ALTER|GRANT|REVOKE|CREATE\s+DATABASE|CREATE\s+SCHEMA)\b`)

	// 危险 SQL 函数 (读取文件、大对象等)，在只读查询中也需拦截。
	reDangerousFunctions = regexp.MustCompile(
		`(?i)\b(pg_read_file|pg_read_binary_file|pg_ls_dir|pg_stat_file|` +
			`lo_import|lo_export|lo_get|lo_put|` +
			`pg_execute_server_program|dblink|dblink_exec)\b`)

	// 执行白名单 (首关键词必须是这些)。
	executeWhitelist = map[string]bool{
		"INSERT": true, "UPDATE": true, "DELETE": true, "MERGE": true,
	}

	// 分号分割语句。
	reSemicolon = regexp.MustCompile(`;\s*$`)
)

// stripSQLLiterals 去除 SQL 字符串字面量 (对应 Python _strip_sql_literals)。
// 避免在内容 'DROP TABLE' 上误报。
func stripSQLLiterals(sql string) string {
	return reLiteral.ReplaceAllString(sql, "''")
}

// validateSingleStatement 验证只包含单条 SQL (对应 Python _validate_single_statement)。
func validateSingleStatement(sql string) error {
	// 去除末尾分号后，若还有分号则为多语句
	trimmed := strings.TrimSpace(sql)
	trimmed = reSemicolon.ReplaceAllString(trimmed, "")
	if strings.Contains(trimmed, ";") {
		return ErrMultiStatement
	}
	return nil
}

// firstSQLKeyword 提取 SQL 首关键词 (对应 Python _first_sql_keyword)。
func firstSQLKeyword(sql string) string {
	if m := reFirstKeyword.FindStringSubmatch(sql); len(m) == 2 {
		return strings.ToUpper(m[1])
	}
	return ""
}

// ValidateReadOnlyQuery 验证只读查询 (对应 Python _validate_read_only_query)。
// 先去除字面量再检测写入关键词和危险函数。
func ValidateReadOnlyQuery(sql string) error {
	if err := validateSingleStatement(sql); err != nil {
		return err
	}
	stripped := stripSQLLiterals(sql)
	if reWriteKeywords.MatchString(stripped) {
		return ErrReadOnlyViolation
	}
	if reDangerousFunctions.MatchString(stripped) {
		return ErrDangerousSQL
	}
	return nil
}

// ValidateExecuteQuery 验证执行语句 (对应 Python _validate_execute_query)。
// 白名单首关键词 + 危险模式检测。
func ValidateExecuteQuery(sql string) error {
	if err := validateSingleStatement(sql); err != nil {
		return err
	}
	keyword := firstSQLKeyword(sql)
	if !executeWhitelist[keyword] {
		return ErrDangerousSQL
	}
	stripped := stripSQLLiterals(sql)
	if reDangerousExec.MatchString(stripped) {
		return ErrDangerousSQL
	}
	return nil
}
