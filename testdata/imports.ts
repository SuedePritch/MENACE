import defaultHelper, { helperFn, CONSTANT } from './imported';

function main() {
  const value = helperFn(CONSTANT);
  const msg = defaultHelper();
  console.log(value, msg);
}

export { main };
