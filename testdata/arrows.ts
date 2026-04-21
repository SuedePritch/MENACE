// Basic arrow function
const noop = () => {};

// Async arrow function
const fetchData = async () => {
  return await Promise.resolve("data");
};

// Exported typed arrow function
export const transform = (input: string): string => {
  return input.toUpperCase();
};

// Arrow with complex types
export const processItems = async (items: Array<string>): Promise<number> => {
  return items.length;
};

// Function containing a nested arrow
function outerFunction() {
  const innerArrow = (x: number) => {
    return x * 2;
  };
  return innerArrow(5);
}
