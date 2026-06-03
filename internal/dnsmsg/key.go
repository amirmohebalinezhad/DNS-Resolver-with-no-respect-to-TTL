package dnsmsg

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/miekg/dns"
)

func QuestionKey(msg *dns.Msg) (string, dns.Question, bool) {
	if msg == nil || len(msg.Question) == 0 {
		return "", dns.Question{}, false
	}
	q := msg.Question[0]
	return KeyFromQuestion(q), q, true
}

func KeyFromQuestion(q dns.Question) string {
	return fmt.Sprintf(
		"%s|%s|%s",
		strings.ToLower(dns.Fqdn(q.Name)),
		typeName(q.Qtype),
		className(q.Qclass),
	)
}

func HasTXTQuestion(msg *dns.Msg) bool {
	return HasQuestionType(msg, dns.TypeTXT)
}

func HasAAAAQuestion(msg *dns.Msg) bool {
	return HasQuestionType(msg, dns.TypeAAAA)
}

func HasQuestionType(msg *dns.Msg, qtype uint16) bool {
	if msg == nil {
		return false
	}
	for _, q := range msg.Question {
		if q.Qtype == qtype {
			return true
		}
	}
	return false
}

func BlockedQuestion(msg *dns.Msg, blockTXT, blockAAAA bool) (uint16, string, bool) {
	if msg == nil {
		return 0, "", false
	}
	for _, q := range msg.Question {
		if blockTXT && q.Qtype == dns.TypeTXT {
			return q.Qtype, typeName(q.Qtype), true
		}
		if blockAAAA && q.Qtype == dns.TypeAAAA {
			return q.Qtype, typeName(q.Qtype), true
		}
	}
	return 0, "", false
}

func QuestionNameAndType(msg *dns.Msg) (string, string) {
	if msg == nil || len(msg.Question) == 0 {
		return "", ""
	}
	q := msg.Question[0]
	return strings.ToLower(dns.Fqdn(q.Name)), typeName(q.Qtype)
}

func typeName(qtype uint16) string {
	if name, ok := dns.TypeToString[qtype]; ok {
		return name
	}
	return strconv.Itoa(int(qtype))
}

func className(qclass uint16) string {
	if name, ok := dns.ClassToString[qclass]; ok {
		return name
	}
	return strconv.Itoa(int(qclass))
}
