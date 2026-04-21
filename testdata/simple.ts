// Named function (unexported)
function greet(name: string): string {
  return "Hello, " + name;
}

// Exported named function
export function add(a: number, b: number): number {
  return a + b;
}

// Export default function
export default function main() {
  const result = add(1, 2);
  console.log(greet("World"));
  console.log(result);
}
