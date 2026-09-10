import { defineAdmin } from '@riducms/plugin/admin';
import { generatedAdminPlugins } from './ridu.plugins.generated';
import BrandLogo from './components/brand-logo.svelte';
import SupportLink from './components/support-link.svelte';
import LoginMessage from './components/login-message.svelte';

export default defineAdmin({
	plugins: generatedAdminPlugins,
	branding: [
		{ key: 'login-logo', surface: 'loginLogo', component: BrandLogo },
		{
			key: 'nav-logo',
			surface: 'navigationLogo',
			component: BrandLogo
		}
	],
	navigation: [
		{ key: 'support', component: SupportLink, position: 'afterLinks' }
	],
	login: [
		{
			key: 'work-email',
			component: LoginMessage,
			position: 'replace'
		}
	]
});
