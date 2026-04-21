// Interface declaration
interface User {
  id: number;
  name: string;
  email: string;
}

// Type alias
type Config = {
  host: string;
  port: number;
  debug: boolean;
};

// Exported type
export type ApiResponse<T> = {
  data: T;
  error: string | null;
  status: number;
};

// Exported interface
export interface Repository<T> {
  find(id: number): T | null;
  findAll(): T[];
  save(entity: T): void;
  delete(id: number): boolean;
}

// Enum
enum Direction {
  Up = "UP",
  Down = "DOWN",
  Left = "LEFT",
  Right = "RIGHT",
}

// Exported enum
export enum HttpStatus {
  OK = 200,
  NotFound = 404,
  ServerError = 500,
}
