function log(_target: any, _key: string, _descriptor: PropertyDescriptor) {}

export class UserService {
  private db: any;
  public name: string;

  constructor(db: any) {
    this.db = db;
    this.name = "UserService";
  }

  public getUser(id: number): any {
    return this.db.find(id);
  }

  private hashPassword(password: string): string {
    return password.split("").reverse().join("");
  }

  static create(): UserService {
    return new UserService(null);
  }

  get displayName(): string {
    return this.name;
  }

  set displayName(value: string) {
    this.name = value;
  }

  @log
  async fetchRemoteUser(id: number): Promise<any> {
    return await fetch(`/api/users/${id}`);
  }

  protected internalMethod(): void {
    this.hashPassword("secret");
  }
}
