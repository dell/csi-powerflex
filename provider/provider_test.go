package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNew(t *testing.T) {
	// Call the New function
	result := New()

	// Verify that the result is as expected
	// For now, we just want to make sure a plugin was created, all the New() function does is
	// set up function pointers
	assert.NotNil(t, result)
}
