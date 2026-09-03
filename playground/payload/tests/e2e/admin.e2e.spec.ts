import { test, expect, Page } from '@playwright/test'
import { login } from '../helpers/login'
import { seedTestUser, cleanupTestUser, testUser } from '../helpers/seedUser'

test.describe('Admin Panel', () => {
  let page: Page

  test.beforeAll(async ({ browser }) => {
    await seedTestUser()

    const context = await browser.newContext()
    page = await context.newPage()

    await login({ page, user: testUser })
  })

  test.afterAll(async () => {
    await cleanupTestUser()
  })

  test('can navigate to dashboard', async () => {
    await page.goto('http://localhost:3100/admin')
    await expect(page).toHaveURL('http://localhost:3100/admin')
    const dashboardArtifact = page.locator('span[title="Dashboard"]').first()
    await expect(dashboardArtifact).toBeVisible()
  })

  test('can navigate to list view', async () => {
    await page.goto('http://localhost:3100/admin/collections/users')
    await expect(page).toHaveURL('http://localhost:3100/admin/collections/users')
    const listViewArtifact = page.locator('h1', { hasText: 'Users' }).first()
    await expect(listViewArtifact).toBeVisible()
  })

  test('can navigate to create view', async () => {
    await page.goto('http://localhost:3100/admin/collections/users/create')
    await expect(page).toHaveURL('http://localhost:3100/admin/collections/users/create')
    const createViewArtifact = page.locator('input[name="email"]')
    await expect(createViewArtifact).toBeVisible()
  })
})
