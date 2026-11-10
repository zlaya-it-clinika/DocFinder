// Проверки полей форм на клиенте. Ядро проверяет то же самое, здесь - чтобы сразу подсказать пользователю.

// Почта: текст, @ и домен с зоной через точку, без пробелов
const emailPattern = /^[^\s@]+@[^\s@.]+(\.[^\s@.]+)+$/;

export function check_IsEmail(value) {
	return emailPattern.test(String(value ?? '').trim());
}
