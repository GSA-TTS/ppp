package secret

import "testing"

func TestResolveCustom_HostMatchReturnsSubstitution(t *testing.T) {
	r, _ := resolverWith(nil)
	r.SetCustom([]CustomSecret{
		{
			Name:        "jira",
			Placeholder: "__PPP_JIRA__",
			Value:       "FAKE-JIRA-TOKEN",
			Hosts:       []string{"jira.example.com"},
		},
	})

	subs, err := r.ResolveCustom("jira.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("expected 1 substitution, got %d", len(subs))
	}
	if subs[0].Placeholder != "__PPP_JIRA__" || subs[0].Value != "FAKE-JIRA-TOKEN" {
		t.Errorf("wrong substitution: %+v", subs[0])
	}
}

func TestResolveCustom_NonMatchingHostReturnsNone(t *testing.T) {
	r, _ := resolverWith(nil)
	r.SetCustom([]CustomSecret{
		{
			Name:        "jira",
			Placeholder: "__PPP_JIRA__",
			Value:       "FAKE-JIRA-TOKEN",
			Hosts:       []string{"jira.example.com"},
		},
	})

	subs, err := r.ResolveCustom("api.anthropic.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(subs) != 0 {
		t.Errorf("expected no substitutions for a non-matching host, got %d", len(subs))
	}
}

func TestResolveCustom_MultipleTuplesMatchSameHost(t *testing.T) {
	r, _ := resolverWith(nil)
	r.SetCustom([]CustomSecret{
		{Name: "a", Placeholder: "__A__", Value: "FAKE-A", Hosts: []string{"internal.example.com"}},
		{Name: "b", Placeholder: "__B__", Value: "FAKE-B", Hosts: []string{"internal.example.com", "other.example.com"}},
		{Name: "c", Placeholder: "__C__", Value: "FAKE-C", Hosts: []string{"nope.example.com"}},
	})

	subs, err := r.ResolveCustom("internal.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(subs) != 2 {
		t.Fatalf("expected 2 substitutions, got %d: %+v", len(subs), subs)
	}
}

func TestResolveCustom_HostMatchIsCaseInsensitive(t *testing.T) {
	r, _ := resolverWith(nil)
	r.SetCustom([]CustomSecret{
		{Name: "a", Placeholder: "__A__", Value: "FAKE-A", Hosts: []string{"Jira.Example.com"}},
	})

	subs, err := r.ResolveCustom("jira.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(subs) != 1 {
		t.Errorf("expected case-insensitive host match, got %d", len(subs))
	}
}

func TestResolveCustom_EmptyHostIsError(t *testing.T) {
	r, _ := resolverWith(nil)
	if _, err := r.ResolveCustom(""); err == nil {
		t.Error("expected an error for an empty host")
	}
}
