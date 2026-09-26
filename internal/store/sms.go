package store

import "time"

// SendRequest describes one outbound message.
type SendRequest struct {
	Sender    string
	Recipient string
	Message   string
	Kind      string
}

// advance moves a message along the pending -> processing -> sent -> delivered
// lifecycle based on wall-clock time. Terminal states are never changed.
func (m *SMS) advance(now time.Time, failure bool) {
	switch m.Status {
	case StatusDelivered, StatusFailed, StatusCancelled:
		return
	}

	start := m.CreatedAt
	if m.ScheduledAt != nil && m.ScheduledAt.After(start) {
		start = *m.ScheduledAt
	}
	if now.Before(start) {
		m.Status = StatusPending
		m.StatusDetail = StatusDetailUnknown
		return
	}

	elapsed := now.Sub(start)
	switch {
	case elapsed < processingAfter:
		m.Status = StatusPending
		m.StatusDetail = StatusDetailUnknown
	case elapsed < sentAfter:
		m.Status = StatusProcessing
		m.StatusDetail = StatusDetailSent
	case elapsed < deliveredAfter:
		m.Status = StatusSent
		m.StatusDetail = StatusDetailSent
		if m.SentAt == nil {
			t := start.Add(sentAfter)
			m.SentAt = &t
		}
	default:
		if m.SentAt == nil {
			t := start.Add(sentAfter)
			m.SentAt = &t
		}
		if failure {
			m.Status = StatusFailed
			m.StatusDetail = StatusDetailDeliveryFailed
			detail, category := DetailSubscriberUnreachable, CategorySubscriberUnreachable
			m.Detail, m.DetailCategory = &detail, &category
		} else {
			m.Status = StatusDelivered
			m.StatusDetail = StatusDetailDelivered
			m.Detail, m.DetailCategory = nil, nil
			if m.DeliveredAt == nil {
				t := start.Add(deliveredAfter)
				m.DeliveredAt = &t
			}
		}
	}
}

func (s *Store) advanceLocked() {
	now := s.now()
	for _, id := range s.smsOrder {
		msg := s.sms[id]
		msg.advance(now, s.isFailureLocked(msg.Recipient))
	}
}

func (s *Store) isFailureLocked(recipient string) bool {
	for _, n := range s.cfg.FailureNumbers {
		if n == recipient {
			return true
		}
	}
	return false
}

func (s *Store) ensureSenderLocked(name string) {
	if name == "" {
		return
	}
	for i := range s.senders {
		if s.senders[i].Name == name {
			return
		}
	}
	s.senders = append([]Sender{{Name: name, Status: "APPROVED"}}, s.senders...)
}

func (s *Store) recordLocked(req SendRequest, now time.Time, batchID string) *SMS {
	msg := &SMS{
		ID:           NewMessageID(),
		Kind:         req.Kind,
		Sender:       req.Sender,
		Recipient:    NormalizePhone(req.Recipient),
		RawRecipient: req.Recipient,
		Message:      req.Message,
		Status:       StatusPending,
		StatusDetail: StatusDetailUnknown,
		CreditCost:   1,
		CreatedAt:    now,
		BatchID:      batchID,
	}
	s.ensureSenderLocked(req.Sender)
	msg.advance(now, s.isFailureLocked(msg.Recipient))
	s.sms[msg.ID] = msg
	s.smsOrder = append(s.smsOrder, msg.ID)
	return msg
}

// SendSMS records a single outbound message.
func (s *Store) SendSMS(sender, recipient, message, kind string) (*SMS, error) {
	msgs, err := s.SendBatch(sender, []string{recipient}, message, kind)
	if err != nil {
		return nil, err
	}
	return msgs[0], nil
}

// SendBatch records one message per recipient, deducting one credit each.
func (s *Store) SendBatch(sender string, recipients []string, message, kind string) ([]*SMS, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.advanceLocked()

	if sender == "" || message == "" || len(recipients) == 0 {
		return nil, ErrInvalidInput
	}
	for _, r := range recipients {
		if !ValidPhone(r) {
			return nil, ErrInvalidInput
		}
	}
	if s.credit < len(recipients) {
		return nil, ErrInsufficientSMS
	}
	s.credit -= len(recipients)

	now := s.now()
	batchID := ""
	if len(recipients) > 1 {
		batchID = NewMessageID()
	}
	out := make([]*SMS, 0, len(recipients))
	for _, r := range recipients {
		out = append(out, s.recordLocked(SendRequest{
			Sender:    sender,
			Recipient: r,
			Message:   message,
			Kind:      kind,
		}, now, batchID))
	}
	return out, nil
}

// ScheduleSMS records a message to be sent at a future time. Credit is not
// deducted until the message leaves the pending state.
func (s *Store) ScheduleSMS(sender, recipient, message string, at time.Time) (*SMS, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.advanceLocked()

	if sender == "" || message == "" || !ValidPhone(recipient) {
		return nil, ErrInvalidInput
	}
	now := s.now()
	msg := &SMS{
		ID:           newID("sch_"),
		Kind:         KindScheduled,
		Sender:       sender,
		Recipient:    NormalizePhone(recipient),
		RawRecipient: recipient,
		Message:      message,
		Status:       StatusPending,
		StatusDetail: StatusDetailUnknown,
		CreditCost:   1,
		CreatedAt:    now,
		ScheduledAt:  &at,
	}
	s.ensureSenderLocked(sender)
	s.sms[msg.ID] = msg
	s.smsOrder = append(s.smsOrder, msg.ID)
	return msg, nil
}

// SMSList returns recorded messages, newest first.
func (s *Store) SMSList() []*SMS {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.advanceLocked()
	out := make([]*SMS, 0, len(s.smsOrder))
	for i := len(s.smsOrder) - 1; i >= 0; i-- {
		out = append(out, s.sms[s.smsOrder[i]])
	}
	return out
}

// SMSByID looks up a message by ID.
func (s *Store) SMSByID(id string) (*SMS, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.advanceLocked()
	m, ok := s.sms[id]
	return m, ok
}

// DeleteSMS removes a message.
func (s *Store) DeleteSMS(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sms[id]; !ok {
		return false
	}
	delete(s.sms, id)
	s.smsOrder = removeID(s.smsOrder, id)
	return true
}

// ClearSMS removes all messages and returns the number removed.
func (s *Store) ClearSMS() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.smsOrder)
	s.sms = make(map[string]*SMS)
	s.smsOrder = nil
	return n
}

// SMSCount returns the number of recorded messages.
func (s *Store) SMSCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.smsOrder)
}

// Credit returns the remaining SMS balance.
func (s *Store) Credit() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.credit
}

// SetCredit replaces the balance.
func (s *Store) SetCredit(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n < 0 {
		n = 0
	}
	s.credit = n
}

// AddCredit increases the balance by n.
func (s *Store) AddCredit(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.credit += n
	if s.credit < 0 {
		s.credit = 0
	}
}

// Senders returns the known sender names.
func (s *Store) Senders() []Sender {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Sender, len(s.senders))
	copy(out, s.senders)
	return out
}

// Analytics returns today's and this month's usage counters.
func (s *Store) Analytics() (today Stats, thisMonth Stats) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.advanceLocked()

	now := s.now()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())

	for _, id := range s.smsOrder {
		msg := s.sms[id]
		if msg.Kind == KindScheduled {
			continue
		}
		if msg.CreatedAt.Before(monthStart) {
			continue
		}
		thisMonth.add(msg)
		if !msg.CreatedAt.Before(dayStart) {
			today.add(msg)
		}
	}
	return today, thisMonth
}

func (st *Stats) add(msg *SMS) {
	st.Total++
	switch msg.Status {
	case StatusDelivered:
		st.Delivered++
	case StatusFailed:
		st.Failed++
	}
}

// CreateAPIKey generates and stores a new API key.
func (s *Store) CreateAPIKey(name string) *APIKey {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := &APIKey{
		ID:        newID("key_"),
		Name:      name,
		Key:       NewSecret(),
		CreatedAt: s.now(),
	}
	s.apiKeys = append(s.apiKeys, key)
	return key
}

// APIKeys returns all generated API keys.
func (s *Store) APIKeys() []*APIKey {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*APIKey, len(s.apiKeys))
	copy(out, s.apiKeys)
	return out
}
