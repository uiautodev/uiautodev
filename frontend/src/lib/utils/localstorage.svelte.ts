/**
 * Creates a reactive state that syncs with localStorage
 * @param key - The localStorage key
 * @param defaultValue - The default value if no value exists in localStorage
 * @returns A reactive state object with a getter and setter
 */
export function useLocalStorage<T>(key: string, defaultValue: T) {
  // Get initial value from localStorage or use default
  const getInitialValue = (): T => {
    if (typeof window === 'undefined') {
      return defaultValue;
    }

    try {
      const item = window.localStorage.getItem(key);
      return item ? (JSON.parse(item) as T) : defaultValue;
    } catch (error) {
      console.error(`Error reading localStorage key "${key}":`, error);
      return defaultValue;
    }
  };

  let value = $state(getInitialValue());

  return {
    get current(): T {
      return value;
    },
    set current(newValue: T) {
      value = newValue;

      // Save to localStorage
      if (typeof window !== 'undefined') {
        try {
          window.localStorage.setItem(key, JSON.stringify(newValue));
        } catch (error) {
          console.error(`Error setting localStorage key "${key}":`, error);
        }
      }
    }
  };
}
