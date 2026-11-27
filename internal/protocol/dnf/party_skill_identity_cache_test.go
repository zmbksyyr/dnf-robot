package dnf

import (
	"database/sql"
	"testing"

	_ "github.com/go-sql-driver/mysql"
)

// Batch scheduling logs the same account in and out, and every login used to cost
// one charac_info query even though a character's job never changes. The cache is
// keyed by account and character, so a second lookup must not reach the database;
// a closed database proves that by failing if it is queried.
func TestQueryPartySkillProfileCachesIdentity(t *testing.T) {
	db, err := sql.Open("mysql", "invalid:invalid@tcp(127.0.0.1:1)/empty")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	identity := partySkillIdentity{uid: 17000021, cid: 23}
	row := partySkillDBRow{cid: 23, job: 8}
	partySkillIdentityCache.Store(identity, row)
	defer partySkillIdentityCache.Delete(identity)

	profile, err := queryPartySkillProfile(db, 17000021, 23)
	if err != nil {
		t.Fatalf("cached lookup hit the database: %v", err)
	}
	if profile.cid != 23 || profile.job != 8 {
		t.Fatalf("profile = cid %d job %d", profile.cid, profile.job)
	}
	// Candidates depend on the process catalog, which this test does not load;
	// the cached identity is what the database round trip would have provided.
	if profile.whitelistCount != 0 || profile.pvfCount != 0 {
		t.Fatalf("cached profile invented catalog counts: %d/%d", profile.whitelistCount, profile.pvfCount)
	}
}

func TestQueryPartySkillProfileMissQueriesDatabase(t *testing.T) {
	db, err := sql.Open("mysql", "invalid:invalid@tcp(127.0.0.1:1)/empty")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// No cache entry: the lookup must try the database and report its failure
	// instead of inventing a profile.
	if _, err := queryPartySkillProfile(db, 17000099, 99); err == nil {
		t.Fatal("uncached lookup reported success without a database")
	}
}
