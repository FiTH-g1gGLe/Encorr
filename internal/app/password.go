package app

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Passwords are stored as PBKDF2-SHA256 with a random salt and many rounds,
// so a stolen encorr.json doesn't reveal anyone's password.
const pwRounds = 600_000

func hashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, pw, salt, pwRounds, 32)
	if err != nil {
		return "", err
	}
	enc := base64.RawStdEncoding
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", pwRounds, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// dummyHash is checked when the username doesn't exist, so a wrong username
// takes as long as a wrong password (no hints about who has an account).
var dummyHash, _ = hashPassword("encorr-dummy-password")

func checkPassword(hash, pw string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	rounds, err := strconv.Atoi(parts[1])
	if err != nil || rounds < 1000 {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err1 := enc.DecodeString(parts[2])
	want, err2 := enc.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, rounds, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

func validPassword(pw string) error {
	if len([]rune(pw)) < 8 {
		return userErr("Passwords need at least 8 characters.")
	}
	if len(pw) > 200 {
		return userErr("That password is too long.")
	}
	return nil
}

func validUsername(name string) error {
	n := []rune(name)
	if len(n) < 2 || len(n) > 32 {
		return userErr("Usernames need 2 to 32 characters.")
	}
	for _, r := range n {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' || r == '-') {
			return userErr("Usernames can use letters, numbers, dots, dashes and underscores (no spaces).")
		}
	}
	return nil
}

// newPassword makes an easy-to-type password like "maple-river-4821".
func newPassword() string {
	words := []string{"amber", "anchor", "apple", "arrow", "atlas", "autumn", "badger", "banjo", "beacon", "birch", "blossom", "breeze", "bridge", "canyon", "cedar", "comet", "copper", "coral", "cosmos", "cricket", "dawn", "delta", "ember", "falcon", "fern", "flint", "forest", "galaxy", "garnet", "glacier", "harbour", "hazel", "heron", "island", "ivy", "jasper", "juniper", "kestrel", "lagoon", "lantern", "lemon", "lotus", "maple", "meadow", "meteor", "mint", "nebula", "nutmeg", "oak", "ocean", "olive", "orbit", "otter", "pebble", "pepper", "pine", "planet", "puffin", "quartz", "rain", "raven", "reef", "ripple", "river", "robin", "saffron", "sage", "shadow", "silver", "sparrow", "spruce", "storm", "summit", "sunset", "thistle", "thunder", "tiger", "topaz", "tulip", "velvet", "violet", "willow", "winter", "wren"}
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	n := int(b[0])<<8 | int(b[1])
	return fmt.Sprintf("%s-%s-%04d", words[int(b[2])%len(words)], words[int(b[3])%len(words)], n%10000)
}
