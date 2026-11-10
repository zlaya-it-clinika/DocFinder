// Форматирование дат, размеров и имён для интерфейса.
import { FormatTexts, SidebarTexts } from '../texts.js';

const locale = 'ru-RU';
const dayMs = 24 * 60 * 60 * 1000;

const timeFormat = new Intl.DateTimeFormat(locale, { hour: '2-digit', minute: '2-digit' });
const dateTimeFormat = new Intl.DateTimeFormat(locale, {
	day: '2-digit',
	month: '2-digit',
	year: 'numeric',
	hour: '2-digit',
	minute: '2-digit',
});
const monthFormat = new Intl.DateTimeFormat(locale, { month: 'long', year: 'numeric' });

export function parse_Date(value) {
	if (!value) {
		return null;
	}
	const date = new Date(value);
	return Number.isNaN(date.getTime()) ? null : date;
}

export function format_DateTime(value) {
	const date = parse_Date(value);
	return date ? dateTimeFormat.format(date) : '';
}

export function format_Time(value) {
	const date = parse_Date(value);
	return date ? timeFormat.format(date) : '';
}

function start_OfDay(date) {
	return new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime();
}

// Подпись группы для списка чатов: Сегодня, Вчера, 7 дней, 30 дней, месяц
export function get_DateGroupLabel(value, now = new Date()) {
	const date = parse_Date(value);
	if (!date) {
		return SidebarTexts.groupToday;
	}
	const daysAgo = Math.round((start_OfDay(now) - start_OfDay(date)) / dayMs);
	if (daysAgo <= 0) {
		return SidebarTexts.groupToday;
	}
	if (daysAgo === 1) {
		return SidebarTexts.groupYesterday;
	}
	if (daysAgo <= 7) {
		return SidebarTexts.groupWeek;
	}
	if (daysAgo <= 30) {
		return SidebarTexts.groupMonth;
	}
	const label = monthFormat.format(date);
	return label.charAt(0).toUpperCase() + label.slice(1);
}

export function format_FileSize(bytes) {
	if (!Number.isFinite(bytes)) {
		return '';
	}
	if (bytes < 1024) {
		return `${bytes} ${FormatTexts.bytes}`;
	}
	if (bytes < 1024 * 1024) {
		return `${(bytes / 1024).toFixed(1)} ${FormatTexts.kilobytes}`;
	}
	if (bytes < 1024 * 1024 * 1024) {
		return `${(bytes / 1024 / 1024).toFixed(1)} ${FormatTexts.megabytes}`;
	}
	return `${(bytes / 1024 / 1024 / 1024).toFixed(1)} ${FormatTexts.gigabytes}`;
}

// Инициалы для аватара
export function get_Initials(name) {
	const parts = String(name ?? '').trim().split(/\s+/).filter(Boolean);
	if (parts.length === 0) {
		return '?';
	}
	return parts.slice(0, 2).map((part) => part.charAt(0).toUpperCase()).join('');
}

// Короткий заголовок чата из первого вопроса
export function build_ChatTitle(text, maxLength = 60) {
	const singleLine = String(text ?? '').replace(/\s+/g, ' ').trim();
	if (singleLine.length <= maxLength) {
		return singleLine;
	}
	return singleLine.slice(0, maxLength - 1).trimEnd() + '…';
}

// Имя файла без запрещённых символов
export function build_SafeFileName(name) {
	const cleaned = String(name ?? '').replace(/[\\/:*?"<>|\u0000-\u001f]+/g, ' ').replace(/\s+/g, ' ').trim();
	return cleaned || FormatTexts.defaultFileName;
}
