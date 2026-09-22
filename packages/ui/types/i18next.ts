import 'i18next';

declare global {
  interface I18nResources {
    ui: {
      attach_file: string;
      toggle_sidebar: string;
      pagination_previous: string;
      pagination_next: string;
      copy_code: string;
      plain_text: string;
    };
  }
}

declare module 'i18next' {
  interface CustomTypeOptions {
    resources: I18nResources;
    enableSelector: true;
  }
}
