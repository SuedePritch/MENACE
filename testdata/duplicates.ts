// These two functions have identical logic but different variable names.
// They should produce the same structural hash.
function doubleA(n: number): number {
  const result = n * 2;
  return result;
}

function doubleB(x: number): number {
  const output = x * 2;
  return output;
}

// This function has the same computation but different structure (extra variable, extra step).
// It should NOT match the structural hash of doubleA/doubleB.
function doubleC(z: number): number {
  const temp = z;
  const result = temp * 2;
  return result;
}

// Same logic as doubleA/doubleB but with different parameter count — should NOT match.
function doubleD(a: number, _unused: number): number {
  const result = a * 2;
  return result;
}
