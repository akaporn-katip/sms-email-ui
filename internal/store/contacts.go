package store

import (
	"fmt"
	"strings"
)

// AddContact stores a new contact.
func (s *Store) AddContact(name, phone, email, tags string) (*Contact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	name = strings.TrimSpace(name)
	if name == "" || !ValidPhone(phone) {
		return nil, ErrInvalidInput
	}
	if email != "" && !ValidEmail(email) {
		return nil, ErrInvalidInput
	}
	c := &Contact{
		ID:    newID("ct_"),
		Name:  name,
		Phone: NormalizePhone(phone),
		Email: strings.TrimSpace(email),
		Tags:  strings.TrimSpace(tags),
	}
	s.contacts[c.ID] = c
	s.contactOrder = append(s.contactOrder, c.ID)
	return c, nil
}

// Contacts returns a page of contacts and the total count. Newest first.
func (s *Store) Contacts(page, perPage int) ([]*Contact, int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	total := len(s.contactOrder)
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 10
	}
	start := (page - 1) * perPage
	if start >= total {
		return []*Contact{}, total
	}
	end := start + perPage
	if end > total {
		end = total
	}
	out := make([]*Contact, 0, end-start)
	for i := start; i < end; i++ {
		out = append(out, s.contacts[s.contactOrder[total-1-i]])
	}
	return out, total
}

// ContactByID looks up a contact.
func (s *Store) ContactByID(id string) (*Contact, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.contacts[id]
	return c, ok
}

// ImportContacts stores many contacts. Rows with a missing name, an invalid
// phone number or a phone already present are skipped; the second return value
// counts them and the third describes why.
func (s *Store) ImportContacts(in []Contact) (imported int, skipped int, errs []string) {
	errs = []string{}
	for i, c := range in {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			skipped++
			errs = append(errs, fmt.Sprintf("row %d: name is required", i+1))
			continue
		}
		if !ValidPhone(c.Phone) {
			skipped++
			errs = append(errs, fmt.Sprintf("row %d: invalid phone number", i+1))
			continue
		}
		phone := NormalizePhone(c.Phone)

		s.mu.Lock()
		duplicate := false
		for _, id := range s.contactOrder {
			if s.contacts[id].Phone == phone {
				duplicate = true
				break
			}
		}
		if duplicate {
			s.mu.Unlock()
			skipped++
			errs = append(errs, fmt.Sprintf("row %d: duplicate phone number %s", i+1, phone))
			continue
		}
		contact := &Contact{
			ID:    newID("ct_"),
			Name:  name,
			Phone: phone,
			Email: strings.TrimSpace(c.Email),
			Tags:  strings.TrimSpace(c.Tags),
		}
		s.contacts[contact.ID] = contact
		s.contactOrder = append(s.contactOrder, contact.ID)
		s.mu.Unlock()

		imported++
	}
	return imported, skipped, errs
}

// ContactCount returns the number of stored contacts.
func (s *Store) ContactCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.contactOrder)
}

// DeleteContact removes a contact.
func (s *Store) DeleteContact(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.contacts[id]; !ok {
		return false
	}
	delete(s.contacts, id)
	s.contactOrder = removeID(s.contactOrder, id)
	return true
}

// ClearContacts removes all contacts and returns the number removed.
func (s *Store) ClearContacts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.contactOrder)
	s.contacts = make(map[string]*Contact)
	s.contactOrder = nil
	return n
}
