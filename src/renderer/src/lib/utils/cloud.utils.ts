import { get } from "svelte/store";
import { _ } from 'svelte-i18n';

export const ValidateName = (name: string) => {

  if (!name.trim()) {
    return get(_)('#.ValidateName.0');
  } else if (name[0] === ' ') {
    return get(_)('#.ValidateName.1');
  } else if (name[name.length - 1] === ' ') {
    return get(_)('#.ValidateName.2');
  } else if (name.length <= 0 || name.length > 255) {
    return get(_)('#.ValidateName.3');
  }

  return null;
};