// sql_safety.go — SQL 安全验证 (对应 Python agent_ops_store.py 5 个验证函数)。
//
// Python 函数对应:
//
//	_strip_sql_literals  → StripSQLLiterals
//	_validate_single_statement → ValidateSingleStatement
//	_first_sql_keyword → FirstSQLKeyword
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

	// 执行白名单 (首关键词必须是这些)。
	executeWhitelist = map[string]bool{
		"INSERT": true, "UPDATE": true, "DELETE": true, "MERGE": true,
	}

	// 分号分割语句。
	reSemicolon = regexp.MustCompile(`;\s*$`)
)

// StripSQLLiterals 去除 SQL 字符串字面量 (对应 Python _strip_sql_literals)。
// 避免在内容 'DROP TABLE' 上误报。
func StripSQLLiterals(sql string) string {
	return reLiteral.ReplaceAllString(sql, "''")
}

// ValidateSingleStatement 验证只包含单条 SQL (对应 Python _validate_single_statement)。
func ValidateSingleStatement(sql string) error {
	// 去除末尾分号后，若还有分号则为多语句
	trimmed := strings.TrimSpace(sql)
	trimmed = reSemicolon.ReplaceAllString(trimmed, "")
	if strings.Contains(trimmed, ";") {
		return ErrMultiStatement
	}
	return nil
}

// FirstSQLKeyword 提取 SQL 首关键词 (对应 Python _first_sql_keyword)。
func FirstSQLKeyword(sql string) string {
	if m := reFirstKeyword.FindStringSubmatch(sql); len(m) == 2 {
		return strings.ToUpper(m[1])
	}
	return ""
}

// ValidateReadOnlyQuery 验证只读查询 (对应 Python _validate_read_only_query)。
// 先去除字面量再检测写入关键词。
func ValidateReadOnlyQuery(sql string) error {
	if err := ValidateSingleStatement(sql); err != nil {
		return err
	}
	stripped := StripSQLLiterals(sql)
	if reWriteKeywords.MatchString(stripped) {
		return ErrReadOnlyViolation
	}
	return nil
}

// ValidateExecuteQuery 验证执行语句 (对应 Python _validate_execute_query)。
// 白名单首关键词 + 危险模式检测。
func ValidateExecuteQuery(sql string) error {
	if err := ValidateSingleStatement(sql); err != nil {
		return err
	}
	keyword := FirstSQLKeyword(sql)
	if !executeWhitelist[keyword] {
		return ErrDangerousSQL
	}
	stripped := StripSQLLiterals(sql)
	if reDangerousExec.MatchString(stripped) {
		return ErrDangerousSQL
	}
	return nil
}
