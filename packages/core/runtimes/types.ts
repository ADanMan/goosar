// Производный тип "здоровья" рантайма — статус, который видит пользователь

export type RuntimeHealth =
  | 'online' 
  | 'recently_lost' 
  | 'offline' 
  | 'about_to_gc'; 
