package core

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSetValues(t *testing.T) {
	timestamp := time.Now()
	setValues("0.0.0.0", "0123456", "abcdefghijklmnopqrstuvwxyz01234567891", timestamp)
	assert.Equal(t, "0.0.0.0", SemVer)
	assert.Equal(t, "0123456", CommitSha7)
	assert.Equal(t, "abcdefghijklmnopqrstuvwxyz01234567891", CommitSha32)
	assert.Equal(t, timestamp, CommitTime)
}
