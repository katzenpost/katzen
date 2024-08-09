package main

import (
	"github.com/katzenpost/katzenpost/catshadow"
)

type sortedContacts []*catshadow.Contact

func (s sortedContacts) Less(i, j int) bool {
	// sorts contacts with messages most-recent-first, followed by contacts
	// without messages alphabetically
	if s[i].LastMessage == nil && s[j].LastMessage == nil {
		return s[i].Nickname < s[j].Nickname
	} else if s[i].LastMessage == nil {
		return false
	} else if s[j].LastMessage == nil {
		return true
	} else {
		return s[i].LastMessage.Timestamp.After(s[j].LastMessage.Timestamp)
	}
}
func (s sortedContacts) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
func (s sortedContacts) Len() int {
	return len(s)
}
