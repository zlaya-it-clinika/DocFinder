// Проверка файла перед загрузкой: формат и размер берутся из настроек веб-сервера.
import { FileTexts, format_Text } from '../texts.js';
import { format_FileSize } from './format.js';

function get_Extension(fileName) {
	const match = String(fileName ?? '').toLowerCase().match(/\.[^.\\/]+$/);
	return match ? match[0] : '';
}

// Значение атрибута accept для окна выбора файла
export function build_AcceptAttribute(config) {
	return (config.allowedUploadExtensions ?? []).join(',');
}

export function format_AllowedExtensions(config) {
	return (config.allowedUploadExtensions ?? []).join(', ');
}

// Пустая строка - файл подходит, иначе текст причины для пользователя
export function validate_UploadFile(file, config) {
	const allowed = config.allowedUploadExtensions ?? [];
	if (allowed.length > 0 && !allowed.includes(get_Extension(file.name))) {
		return format_Text(FileTexts.typeNotAllowed, { list: format_AllowedExtensions(config) });
	}
	if (config.maxUploadBytes > 0 && file.size > config.maxUploadBytes) {
		return format_Text(FileTexts.tooLarge, { size: format_FileSize(config.maxUploadBytes) });
	}
	if (file.size === 0) {
		return FileTexts.empty;
	}
	return '';
}
