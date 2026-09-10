import { BlocksFeature, lexicalEditor } from '@payloadcms/richtext-lexical'
import type { Block, CollectionConfig, Field } from 'payload'

type WorkshopValues = {
  city?: string | null
  title?: string | null
}

const authenticated = ({ req }: { req: { user: unknown } }) => Boolean(req.user)

const workshopFields = (): Field[] => [
  {
    name: 'city',
    type: 'select',
    required: true,
    options: ['London', 'Bristol'],
    admin: {
      description: 'After editing the title, change this city to refresh its feedback.',
    },
  },
  {
    name: 'title',
    type: 'text',
    label: 'Workshop title',
    required: true,
    validate: (
      value: null | string | undefined,
      { siblingData }: { siblingData: unknown },
    ) => {
      const { city } = siblingData as WorkshopValues

      if (!value?.trim() || !city) {
        return true
      }

      return value.toLocaleLowerCase().includes(city.toLocaleLowerCase())
        ? true
        : `Include ${city} in the workshop title so guests know where to go.`
    },
    admin: {
      description:
        'Include the selected city, for example: London pottery evening. Try removing London, then wait a moment without saving.',
    },
  },
]

const WorkshopCard: Block = {
  slug: 'workshop',
  labels: {
    singular: 'Workshop card',
    plural: 'Workshop cards',
  },
  fields: workshopFields(),
}

export const Workshops: CollectionConfig = {
  slug: 'workshops',
  admin: {
    useAsTitle: 'title',
  },
  access: {
    create: authenticated,
    read: authenticated,
    update: authenticated,
    delete: authenticated,
  },
  fields: [
    ...workshopFields(),
    {
      name: 'sessions',
      type: 'array',
      label: 'Extra sessions',
      fields: workshopFields(),
      admin: {
        description:
          'The same rule works inside each row. Try editing a title and moving that row.',
      },
    },
    {
      name: 'description',
      type: 'richText',
      label: 'Description and embedded workshop card',
      editor: lexicalEditor({
        features: ({ rootFeatures }) => [
          ...rootFeatures,
          BlocksFeature({ blocks: [WorkshopCard] }),
        ],
      }),
      admin: {
        description:
          'Choose Edit on the seeded Workshop card. Its title is checked before you choose Apply.',
      },
    },
  ],
}
