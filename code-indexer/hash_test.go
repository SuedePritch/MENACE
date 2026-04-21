package indexer

import (
	"testing"
)

// --- Normalization ---

func TestNormalizeStripsCommentsAndWhitespace(t *testing.T) {
	a := `function foo() {
  // a comment
  return   1;  /* block */
}`
	b := `function foo() {
  return 1;
}`
	if Normalize(a) != Normalize(b) {
		t.Errorf("expected identical normalized output\na: %s\nb: %s", Normalize(a), Normalize(b))
	}
}

func TestBodyHashIgnoresWhitespaceAndComments(t *testing.T) {
	a := `function foo() {
  // comment
  return   1;
}`
	b := `function foo() { return 1; }`
	ha := ComputeBodyHash(a)
	hb := ComputeBodyHash(b)
	if ha != hb {
		t.Errorf("body hashes should match: %s vs %s", ha, hb)
	}
}

// --- Structural Hashing ---

func TestStructuralHashSameLogicDifferentNames(t *testing.T) {
	a := `function a(n) { const result = n * 2; return result; }`
	b := `function b(z) { const output = z * 2; return output; }`
	ha := ComputeStructuralHash(a)
	hb := ComputeStructuralHash(b)
	if ha != hb {
		t.Errorf("structural hashes should match for same logic:\na=%s\nb=%s", ha, hb)
	}
}

func TestStructuralHashDifferentStructure(t *testing.T) {
	a := `function a(n) { const x = n * 2; return x; }`
	b := `function b(z) { const temp = z; const result = temp * 2; return result; }`
	ha := ComputeStructuralHash(a)
	hb := ComputeStructuralHash(b)
	if ha == hb {
		t.Errorf("structural hashes should differ for different structure: both=%s", ha)
	}
}

func TestStructuralHashDifferentParamCount(t *testing.T) {
	a := `function a(n) { const result = n * 2; return result; }`
	b := `function b(a, unused) { const result = a * 2; return result; }`
	ha := ComputeStructuralHash(a)
	hb := ComputeStructuralHash(b)
	if ha == hb {
		t.Errorf("structural hashes should differ for different param counts: both=%s", ha)
	}
}
