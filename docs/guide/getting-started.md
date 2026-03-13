# Getting Started

This guide will help you get Arrflix up and running on your system.

## Prerequisites

Before you begin, make sure you have:

- **Docker** and **Docker Compose** installed
- Access to your media library directories

## Installation

Create a `docker-compose.yml` file with the following configuration:

```yml
services:
  arrflix:
    image: ghcr.io/kyleaupton/arrflix:latest
    container_name: arrflix
    ports:
      - 8484:8484
    volumes:
      - /path/to/media:/data
      - arrflix_postgres:/var/lib/postgresql/data
    restart: unless-stopped

volumes:
  arrflix_postgres:
```

### Configuration Steps

1. **Update the volume path** - Replace `/path/to/media` with the actual path to your media directory.

2. **Start Arrflix**:

   ```bash
   docker compose up -d
   ```

3. **Access the web interface** at `http://localhost:8484`

4. **Complete onboarding** - Create an admin account and add your TMDB API key. Arrflix uses [TMDB](https://www.themoviedb.org/) to fetch metadata for movies and TV shows. You can get a free API key from your [TMDB account settings](https://www.themoviedb.org/settings/api).

## Next Steps

Once Arrflix is running, you can:

- Create an admin account and add your TMDB API key
- Configure your [media libraries](./libraries)
- Add [indexers](./indexers) and [download clients](./downloaders)
- Start downloading your favorite shows and movies
