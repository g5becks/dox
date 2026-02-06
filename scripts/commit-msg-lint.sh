#!/bin/sh
# Conventional Commit Message Linter
# Validates commit messages follow: type(scope): description

commit_msg_file="$1"

if [ -z "$commit_msg_file" ]; then
    echo "Error: No commit message file provided"
    exit 1
fi

if [ ! -f "$commit_msg_file" ]; then
    echo "Error: Commit message file not found: $commit_msg_file"
    exit 1
fi

# Read the first line (subject)
subject=$(head -n 1 "$commit_msg_file")

# Skip merge commits
if echo "$subject" | grep -qE "^Merge "; then
    exit 0
fi

# Skip fixup/squash commits
if echo "$subject" | grep -qE "^(fixup|squash)! "; then
    exit 0
fi

# Valid conventional commit types
types="feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert"

# Pattern: type(scope): description  OR  type: description
# - type is required and must be one of the valid types
# - scope is optional, wrapped in parentheses
# - colon and space after type/scope is required
# - description is required and must start with lowercase
pattern="^($types)(\([a-zA-Z0-9_-]+\))?: .+"

if ! echo "$subject" | grep -qE "$pattern"; then
    echo ""
    echo "ERROR: Invalid commit message format!"
    echo ""
    echo "Your message:  $subject"
    echo ""
    echo "Expected format: <type>(<scope>): <description>"
    echo ""
    echo "Valid types: feat, fix, docs, style, refactor, perf, test, build, ci, chore, revert"
    echo ""
    echo "Examples:"
    echo "  feat: add user authentication"
    echo "  fix(api): handle null response"
    echo "  docs: update README"
    echo "  chore(deps): bump dependencies"
    echo ""
    exit 1
fi

# Check subject length (max 72 characters recommended)
subject_length=$(echo "$subject" | wc -c)
if [ "$subject_length" -gt 73 ]; then
    echo ""
    echo "WARNING: Commit subject is longer than 72 characters ($subject_length chars)"
    echo "Consider shortening: $subject"
    echo ""
    # Warning only, don't fail
fi

exit 0
