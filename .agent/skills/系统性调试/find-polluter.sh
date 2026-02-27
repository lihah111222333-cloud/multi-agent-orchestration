#!/usr/bin/env bash
# Bisection script to find which test creates unwanted files/state
# Usage: ./find-polluter.sh <file_or_dir_to_check> <test_pattern>
# Example (Go):  ./find-polluter.sh '.git' './internal/...'
# Example (Node): ./find-polluter.sh '.git' 'src/**/*.test.ts'

set -e

if [ $# -ne 2 ]; then
  echo "Usage: $0 <file_to_check> <test_pattern>"
  echo "Example (Go):  $0 '.git' './internal/...'"
  echo "Example (Node): $0 '.git' 'src/**/*.test.ts'"
  exit 1
fi

POLLUTION_CHECK="$1"
TEST_PATTERN="$2"

# Auto-detect test runner
if [ -f "go.mod" ]; then
  TEST_CMD="go test"
  TEST_FILES=$(go test -list '.*' "$TEST_PATTERN" 2>/dev/null | grep -v '^ok' | grep -v '^$' || find . -path "$TEST_PATTERN" -name '*_test.go' | sort)
elif [ -f "package.json" ]; then
  TEST_CMD="npm test"
  TEST_FILES=$(find . -path "$TEST_PATTERN" | sort)
else
  echo "❌ Cannot detect project type (no go.mod or package.json)"
  exit 1
fi

echo "🔍 Searching for test that creates: $POLLUTION_CHECK"
echo "Test runner: $TEST_CMD"
echo "Test pattern: $TEST_PATTERN"
echo ""

TOTAL=$(echo "$TEST_FILES" | wc -l | tr -d ' ')
echo "Found $TOTAL test files"
echo ""

COUNT=0
for TEST_FILE in $TEST_FILES; do
  COUNT=$((COUNT + 1))

  # Skip if pollution already exists
  if [ -e "$POLLUTION_CHECK" ]; then
    echo "⚠️  Pollution already exists before test $COUNT/$TOTAL"
    echo "   Skipping: $TEST_FILE"
    continue
  fi

  echo "[$COUNT/$TOTAL] Testing: $TEST_FILE"

  # Run the test
  $TEST_CMD "$TEST_FILE" > /dev/null 2>&1 || true

  # Check if pollution appeared
  if [ -e "$POLLUTION_CHECK" ]; then
    echo ""
    echo "🎯 FOUND POLLUTER!"
    echo "   Test: $TEST_FILE"
    echo "   Created: $POLLUTION_CHECK"
    echo ""
    echo "Pollution details:"
    ls -la "$POLLUTION_CHECK"
    echo ""
    echo "To investigate:"
    echo "  $TEST_CMD $TEST_FILE    # Run just this test"
    echo "  cat $TEST_FILE          # Review test code"
    exit 1
  fi
done

echo ""
echo "✅ No polluter found - all tests clean!"
exit 0
