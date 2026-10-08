import { RIDU_URL } from "$app/env/private";

import { createClient } from "#lib/ridu.generated.js";

export const ridu = createClient({ baseURL: RIDU_URL });
