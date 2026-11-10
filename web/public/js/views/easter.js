import { create_Element, create_Logo } from '../lib/dom.js';
import { navigate_To } from '../router.js';
import { EasterTexts } from '../texts.js';

export function create_EasterView() {
	const element = create_Element('main', { className: 'auth-page' }, [
		create_Element('div', { className: 'auth-card' }, [
			create_Element('div', { className: 'auth-header' }, [
				create_Logo(),
				create_Element('h1', { className: 'auth-title', text: EasterTexts.title }),
				create_Element('p', { className: 'auth-lead', text: EasterTexts.lead }),
			]),
			create_Element('pre', { className: 'easter-query', text: EasterTexts.query }),
			create_Element('button', {
				className: 'button button-primary button-block',
				attrs: { type: 'button' },
				on: { click: () => navigate_To('/login') },
			}, [EasterTexts.back]),
		]),
	]);
	return { element };
}
