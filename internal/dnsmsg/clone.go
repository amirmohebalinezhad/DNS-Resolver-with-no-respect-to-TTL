package dnsmsg

import "github.com/miekg/dns"

func CloneWithIDAndTTL(wire []byte, id uint16, ttl uint32) (*dns.Msg, error) {
	msg := new(dns.Msg)
	if err := msg.Unpack(wire); err != nil {
		return nil, err
	}
	msg.Id = id
	AdjustTTLs(msg, ttl)
	return msg, nil
}
