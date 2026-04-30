export interface ILangfuseConfig {
  secret_key: string;
  public_key: string;
  host: string;
  project_id: string;
  project_name: string;
}

export interface ISystemConfig {
  disablePasswordLogin: boolean;
  registerEnabled: number;
  upload_size_limit: number;
}
