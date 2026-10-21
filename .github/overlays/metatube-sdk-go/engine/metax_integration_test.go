package engine

import "testing"

func TestMetaXNativeProvidersAreRegistered(t *testing.T) {
	e := New(nil)
	for _, name := range []string{"Airav", "AVSOX", "FC2Club", "Freejavbt", "GIGA", "IQQTV", "Jav321", "JavDB", "JavLibrary", "Prestige", "XCity"} {
		if !e.IsMovieProvider(name) {
			t.Errorf("movie provider %s was not discovered by the SDK engine", name)
		}
	}
	for _, name := range []string{"AVBaseActor", "JavDBActor", "MinnanoActor", "WikipediaActor"} {
		if !e.IsActorProvider(name) {
			t.Errorf("actor provider %s was not discovered by the SDK engine", name)
		}
	}
}
