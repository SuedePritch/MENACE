// Test fixture for sub-function pattern duplicate detection.
// Multiple functions share the same internal code patterns.

function processUser(user: any) {
  if (!user.name) {
    throw new Error("name is required");
  }
  const normalized = user.name.trim().toLowerCase();
  const result = normalized.replace(/[^a-z]/g, "");
  console.log("processed:", result);
  return result;
}

function processProduct(product: any) {
  if (!product.title) {
    throw new Error("title is required");
  }
  const normalized = product.title.trim().toLowerCase();
  const result = normalized.replace(/[^a-z]/g, "");
  console.log("processed:", result);
  return result;
}

function processCategory(category: any) {
  if (!category.label) {
    throw new Error("label is required");
  }
  const normalized = category.label.trim().toLowerCase();
  const result = normalized.replace(/[^a-z]/g, "");
  console.log("processed:", result);
  return result;
}

// Different structure — should NOT match the pattern above
function formatData(input: string) {
  const parts = input.split(",");
  const mapped = parts.map((p: string) => p.trim());
  return mapped.join(" | ");
}

// Pattern inside nested blocks
function handleRequest(req: any) {
  if (req.type === "admin") {
    if (!req.name) {
      throw new Error("name is required");
    }
    const normalized = req.name.trim().toLowerCase();
    const result = normalized.replace(/[^a-z]/g, "");
    console.log("processed:", result);
    return result;
  }
  return null;
}

// Another function with a nested matching pattern
function handleEvent(event: any) {
  try {
    if (!event.label) {
      throw new Error("label is required");
    }
    const normalized = event.label.trim().toLowerCase();
    const result = normalized.replace(/[^a-z]/g, "");
    console.log("processed:", result);
    return result;
  } catch (e) {
    return null;
  }
}
