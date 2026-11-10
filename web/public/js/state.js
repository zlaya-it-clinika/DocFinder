// Общее состояние приложения и подписка на изменения.

export const appState = {
	config: null,
	user: null,
};

const listeners = new Set();

export function update_State(patch) {
	Object.assign(appState, patch);
	for (const listener of listeners) {
		listener(appState);
	}
}

export function subscribe_State(listener) {
	listeners.add(listener);
	return () => listeners.delete(listener);
}
