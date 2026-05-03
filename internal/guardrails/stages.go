package guardrails

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// PIIPattern defines a built-in PII detection pattern.
type PIIPattern struct {
	Name        string
	Pattern     *regexp.Regexp
	Replacement string
}

// Built-in PII patterns.
var builtinPII = []PIIPattern{
	{
		Name:        "credit_card",
		Pattern:     regexp.MustCompile(`\b\d{4}[-\s]?\d{4}[-\s]?\d{4}[-\s]?\d{4}\b`),
		Replacement: "[REDACTED_CC]",
	},
	{
		Name:        "ssn",
		Pattern:     regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),
		Replacement: "[REDACTED_SSN]",
	},
	{
		Name:        "email",
		Pattern:     regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`),
		Replacement: "[REDACTED_EMAIL]",
	},
	{
		Name:        "phone",
		Pattern:     regexp.MustCompile(`\b(\+?1[-.\s]?)?\(?\d{3}\)?[-.\s]?\d{3}[-.\s]?\d{4}\b`),
		Replacement: "[REDACTED_PHONE]",
	},
	{
		Name:        "ip_address",
		Pattern:     regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`),
		Replacement: "[REDACTED_IP]",
	},
}

// PIIStage detects personally identifiable information in messages.
// Supports block or redact action for configurable PII types.
type PIIStage struct {
	types    []string // Which PII types to check
	action   ResultAction
	patterns []PIIPattern
}

// NewPIIStage creates a PII detection stage.
// types specifies which PII types to check (credit_card, ssn, email, phone, ip_address).
// action is either Block or Redact.
func NewPIIStage(types []string, action string) (*PIIStage, error) {
	actionVal := Redact
	if strings.EqualFold(action, "block") {
		actionVal = Block
	}

	patterns := filterPIIPatterns(types)
	if len(patterns) == 0 {
		patterns = builtinPII // Default: all types
	}

	return &PIIStage{
		types:    types,
		action:   actionVal,
		patterns: patterns,
	}, nil
}

func (s *PIIStage) Name() string { return "pii" }

func (s *PIIStage) CheckInput(_ context.Context, messages []Message) (Result, error) {
	found := false
	result := make([]Message, len(messages))
	for i, msg := range messages {
		content := msg.Content
		for _, p := range s.patterns {
			if p.Pattern.MatchString(content) {
				found = true
				if s.action == Redact {
					content = p.Pattern.ReplaceAllString(content, p.Replacement)
				}
			}
		}
		result[i] = Message{Role: msg.Role, Content: content}
	}

	if !found {
		return Result{Action: Pass}, nil
	}

	if s.action == Block {
		return Result{
			Action:    Block,
			Message:   "PII detected in request",
			StageName: s.Name(),
		}, nil
	}

	return Result{
		Action:           Redact,
		RedactedMessages: result,
		StageName:        s.Name(),
	}, nil
}

func (s *PIIStage) CheckOutput(_ context.Context, content string) (Result, error) {
	found := false
	result := content
	for _, p := range s.patterns {
		if p.Pattern.MatchString(result) {
			found = true
			if s.action == Redact {
				result = p.Pattern.ReplaceAllString(result, p.Replacement)
			}
		}
	}

	if !found {
		return Result{Action: Pass}, nil
	}

	if s.action == Block {
		return Result{
			Action:    Block,
			Message:   "PII detected in response",
			StageName: s.Name(),
		}, nil
	}

	return Result{
		Action:    Redact,
		Message:   result,
		StageName: s.Name(),
	}, nil
}

func filterPIIPatterns(types []string) []PIIPattern {
	if len(types) == 0 {
		return builtinPII
	}
	typeSet := make(map[string]bool, len(types))
	for _, t := range types {
		typeSet[strings.ToLower(t)] = true
	}
	var filtered []PIIPattern
	for _, p := range builtinPII {
		if typeSet[p.Name] {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

// RegexRule defines a single regex pattern for the regex stage.
type RegexRule struct {
	Name        string `yaml:"name"`
	Pattern     string `yaml:"pattern"`
	Action      string `yaml:"action"`      // block, redact, log
	Replacement string `yaml:"replacement"` // replacement for redact action
	compiled    *regexp.Regexp
}

// RegexStage matches content against configurable regex patterns.
type RegexStage struct {
	rules    []RegexRule
	blockMsg string
}

// NewRegexStage creates a regex guardrail stage.
func NewRegexStage(rules []RegexRule, blockMsg string) (*RegexStage, error) {
	compiled := make([]RegexRule, len(rules))
	for i, r := range rules {
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regex %q: %w", r.Name, err)
		}
		compiled[i] = r
		compiled[i].compiled = re
	}
	if blockMsg == "" {
		blockMsg = "request contains prohibited content"
	}
	return &RegexStage{rules: compiled, blockMsg: blockMsg}, nil
}

func (s *RegexStage) Name() string { return "regex" }

func (s *RegexStage) CheckInput(_ context.Context, messages []Message) (Result, error) {
	redacted := make([]Message, len(messages))
	copy(redacted, messages)
	anyMatch := false

	for _, rule := range s.rules {
		for i, msg := range redacted {
			if rule.compiled.MatchString(msg.Content) {
				anyMatch = true
				switch strings.ToLower(rule.Action) {
				case "block":
					msg := s.blockMsg
					if rule.Name != "" {
						msg = fmt.Sprintf("%s: %s", s.blockMsg, rule.Name)
					}
					return Result{Action: Block, Message: msg, StageName: s.Name()}, nil
				case "redact":
					replacement := rule.Replacement
					if replacement == "" {
						replacement = "[REDACTED]"
					}
					redacted[i] = Message{Role: msg.Role, Content: rule.compiled.ReplaceAllString(msg.Content, replacement)}
				}
			}
		}
	}

	if !anyMatch {
		return Result{Action: Pass}, nil
	}
	return Result{Action: Redact, RedactedMessages: redacted, StageName: s.Name()}, nil
}

func (s *RegexStage) CheckOutput(_ context.Context, content string) (Result, error) {
	result := content
	anyMatch := false

	for _, rule := range s.rules {
		if rule.compiled.MatchString(result) {
			anyMatch = true
			switch strings.ToLower(rule.Action) {
			case "block":
				msg := s.blockMsg
				if rule.Name != "" {
					msg = fmt.Sprintf("%s: %s", s.blockMsg, rule.Name)
				}
				return Result{Action: Block, Message: msg, StageName: s.Name()}, nil
			case "redact":
				replacement := rule.Replacement
				if replacement == "" {
					replacement = "[REDACTED]"
				}
				result = rule.compiled.ReplaceAllString(result, replacement)
			}
		}
	}

	if !anyMatch {
		return Result{Action: Pass}, nil
	}
	return Result{Action: Redact, Message: result, StageName: s.Name()}, nil
}

// KeywordStage performs basic keyword blocklist matching.
// This is a simple substring check, not a prompt injection detector.
type KeywordStage struct {
	blocklist []string
	action    ResultAction
	blockMsg  string
}

// NewKeywordStage creates a keyword blocklist stage.
func NewKeywordStage(blocklist []string, action string, blockMsg string) *KeywordStage {
	actionVal := Block
	if strings.EqualFold(action, "log") {
		actionVal = Pass // Log is just pass with logging
	}
	if blockMsg == "" {
		blockMsg = "request contains blocked content"
	}
	return &KeywordStage{
		blocklist: blocklist,
		action:    actionVal,
		blockMsg:  blockMsg,
	}
}

func (s *KeywordStage) Name() string { return "keyword" }

func (s *KeywordStage) CheckInput(_ context.Context, messages []Message) (Result, error) {
	for _, msg := range messages {
		lower := strings.ToLower(msg.Content)
		for _, kw := range s.blocklist {
			if strings.Contains(lower, strings.ToLower(kw)) {
				return Result{
					Action:    s.action,
					Message:   s.blockMsg,
					StageName: s.Name(),
				}, nil
			}
		}
	}
	return Result{Action: Pass}, nil
}

func (s *KeywordStage) CheckOutput(_ context.Context, content string) (Result, error) {
	lower := strings.ToLower(content)
	for _, kw := range s.blocklist {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return Result{
				Action:    s.action,
				Message:   s.blockMsg,
				StageName: s.Name(),
			}, nil
		}
	}
	return Result{Action: Pass}, nil
}

// LengthStage enforces min/max character or token length limits.
type LengthStage struct {
	maxChars  int
	minChars  int
	direction string // "input" or "output"
	blockMsg  string
}

// NewLengthStage creates a length validation stage.
func NewLengthStage(minChars, maxChars int, direction string) *LengthStage {
	return &LengthStage{
		minChars:  minChars,
		maxChars:  maxChars,
		direction: direction,
	}
}

func (s *LengthStage) Name() string { return "length" }

func (s *LengthStage) CheckInput(_ context.Context, messages []Message) (Result, error) {
	totalChars := 0
	for _, msg := range messages {
		totalChars += len(msg.Content)
	}
	if s.minChars > 0 && totalChars < s.minChars {
		return Result{
			Action:    Block,
			Message:   fmt.Sprintf("input too short: %d chars (minimum %d)", totalChars, s.minChars),
			StageName: s.Name(),
		}, nil
	}
	if s.maxChars > 0 && totalChars > s.maxChars {
		return Result{
			Action:    Block,
			Message:   fmt.Sprintf("input too long: %d chars (maximum %d)", totalChars, s.maxChars),
			StageName: s.Name(),
		}, nil
	}
	return Result{Action: Pass}, nil
}

func (s *LengthStage) CheckOutput(_ context.Context, content string) (Result, error) {
	n := len(content)
	if s.minChars > 0 && n < s.minChars {
		return Result{
			Action:    Block,
			Message:   fmt.Sprintf("output too short: %d chars (minimum %d)", n, s.minChars),
			StageName: s.Name(),
		}, nil
	}
	if s.maxChars > 0 && n > s.maxChars {
		return Result{
			Action:    Block,
			Message:   fmt.Sprintf("output too long: %d chars (maximum %d)", n, s.maxChars),
			StageName: s.Name(),
		}, nil
	}
	return Result{Action: Pass}, nil
}
