ALTER TABLE settings
    ADD COLUMN availability JSONB NOT NULL DEFAULT '{
      "weekly": {
        "1": {"start": "10:00", "end": "19:00"},
        "2": {"start": "10:00", "end": "19:00"},
        "3": {"start": "10:00", "end": "19:00"},
        "4": {"start": "10:00", "end": "19:00"},
        "5": {"start": "10:00", "end": "19:00"},
        "6": {"start": "09:00", "end": "20:00"},
        "7": {"start": "09:00", "end": "20:00"}
      },
      "min_opening_minutes": 120,
      "turnaround_minutes": 60
    }'::jsonb;
