// Named export
export function helperFn(x: number): number {
  return x + 1;
}

// Another named export
export const CONSTANT = 42;

// Default export
export default function defaultHelper(): string {
  return "default";
}
